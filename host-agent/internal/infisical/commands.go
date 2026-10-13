package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var resourceSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var ErrInvalidCommand = errors.New("invalid Infisical command")
var ErrProjectDenied = errors.New("Infisical project is not in the host allowlist")
var ErrConfirmation = errors.New("exact Infisical command confirmation required")

// CommandAuthorizer resolves a trusted MCP context and applies dynamic policy.
// It returns the command with its project target resolved and the current
// identity/host-policy intersection for list filtering.
type CommandAuthorizer interface {
	Authorize(context.Context, Command) (Command, map[string]bool, error)
}

// Command is the common, versioned host/MCP/cluster contract. Value is input
// only: results and logs must never serialize it. Names are always scoped to
// an explicit project, environment, and path where the upstream API needs it.
type Command struct {
	Operation   string           `json:"operation"`
	ProjectID   string           `json:"projectId,omitempty"`
	ID          string           `json:"id,omitempty"`
	IdentityID  string           `json:"identityId,omitempty"`
	Environment string           `json:"environment,omitempty"`
	Path        string           `json:"path,omitempty"`
	Name        string           `json:"name,omitempty"`
	Slug        string           `json:"slug,omitempty"`
	Description string           `json:"description,omitempty"`
	Value       *string          `json:"value,omitempty"`
	Permissions []PermissionRule `json:"permissions,omitempty"`
	Roles       []string         `json:"roles,omitempty"`
	Confirm     string           `json:"confirm,omitempty"`
}

type PermissionRule struct {
	Subject string `json:"subject"`
	Action  string `json:"action"`
}

// Resource is deliberately value-free, even for a GET secret response.
type Resource struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Slug        string   `json:"slug,omitempty"`
	Path        string   `json:"path,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Roles       []string `json:"roles,omitempty"`
}

type CommandResult struct {
	Operation string     `json:"operation"`
	Target    string     `json:"target"`
	Applied   bool       `json:"applied,omitempty"`
	Items     []Resource `json:"items,omitempty"`
}

// CommandService is the policy boundary before the version-pinned Infisical
// adapter. Its allowlist is separate from the server's bearer authentication.
type CommandService struct {
	Client             *Client
	AllowedProjects    map[string]bool
	AllowProjectCreate bool
	ProjectPolicy      interface{ Projects() map[string]bool }
	MCPPolicy          CommandAuthorizer
	ReadOnly           bool
}

func (s CommandService) Execute(ctx context.Context, command Command) (CommandResult, error) {
	if s.Client == nil {
		return CommandResult{}, ErrNotConfigured
	}
	if s.MCPPolicy != nil {
		resolved, allowed, err := s.MCPPolicy.Authorize(ctx, command)
		if err != nil {
			return CommandResult{}, err
		}
		command = resolved
		// Every configured/static boundary is an intersection. Dynamic policy
		// cannot expand a narrower legacy allowlist or host-bridge view.
		for id := range allowed {
			if len(s.AllowedProjects) > 0 && !s.AllowedProjects[id] {
				delete(allowed, id)
			}
		}
		if s.ProjectPolicy != nil {
			hostAllowed := s.ProjectPolicy.Projects()
			for id := range allowed {
				if !hostAllowed[id] {
					delete(allowed, id)
				}
			}
		}
		s.AllowedProjects = allowed
		if command.Operation == "project.create" {
			return CommandResult{}, ErrProjectDenied
		}
	}
	if s.ReadOnly && !ReadOperation(command.Operation) {
		return CommandResult{}, ErrProjectDenied
	}
	if s.ProjectPolicy != nil && s.MCPPolicy == nil {
		s.AllowedProjects = s.ProjectPolicy.Projects()
	}
	target, write, err := validateCommand(command)
	if err != nil {
		return CommandResult{}, err
	}
	if command.Operation == "project.create" {
		if !s.AllowProjectCreate {
			return CommandResult{}, ErrProjectDenied
		}
	} else if command.Operation == "project.list" {
		if len(s.AllowedProjects) == 0 {
			return CommandResult{}, ErrProjectDenied
		}
	} else if !s.AllowedProjects[command.ProjectID] {
		return CommandResult{}, ErrProjectDenied
	}
	if write && command.Confirm != command.Operation+":"+target {
		return CommandResult{}, ErrConfirmation
	}
	result := CommandResult{Operation: command.Operation, Target: target}
	var body []byte
	switch command.Operation {
	case "project.list":
		body, err = s.Client.get(ctx, "/api/v1/projects", nil)
	case "project.get", "environment.list":
		body, err = s.Client.get(ctx, "/api/v1/projects/"+command.ProjectID, nil)
	case "project.create":
		body, err = s.Client.request(ctx, http.MethodPost, "/api/v1/projects", nil, map[string]any{"projectName": command.Name, "slug": command.Slug, "projectDescription": command.Description, "type": "secret-manager"})
	case "project.rename":
		body, err = s.Client.request(ctx, http.MethodPatch, "/api/v1/projects/"+command.ProjectID, nil, map[string]string{"name": command.Name})
	case "project.update":
		body, err = s.Client.request(ctx, http.MethodPatch, "/api/v1/projects/"+command.ProjectID, nil, map[string]any{"name": command.Name, "description": command.Description})
	case "project.delete":
		body, err = s.Client.request(ctx, http.MethodDelete, "/api/v1/projects/"+command.ProjectID, nil, nil)
	case "environment.create", "environment.update", "environment.delete":
		base := "/api/v1/projects/" + command.ProjectID + "/environments"
		switch command.Operation {
		case "environment.create":
			body, err = s.Client.request(ctx, http.MethodPost, base, nil, map[string]string{"name": command.Name, "slug": command.Slug})
		case "environment.update":
			body, err = s.Client.request(ctx, http.MethodPatch, base+"/"+command.ID, nil, map[string]string{"name": command.Name, "slug": command.Slug})
		case "environment.delete":
			body, err = s.Client.request(ctx, http.MethodDelete, base+"/"+command.ID, nil, nil)
		}
	case "folder.list", "folder.create", "folder.update", "folder.delete":
		fields := map[string]string{"workspaceId": command.ProjectID, "environment": command.Environment, "path": command.Path}
		switch command.Operation {
		case "folder.list":
			body, err = s.Client.get(ctx, "/api/v1/folders", url.Values{"workspaceId": {command.ProjectID}, "environment": {command.Environment}, "path": {command.Path}})
		case "folder.create":
			fields["name"] = command.Name
			fields["description"] = command.Description
			body, err = s.Client.request(ctx, http.MethodPost, "/api/v1/folders", nil, fields)
		case "folder.update":
			fields["name"] = command.Name
			fields["description"] = command.Description
			body, err = s.Client.request(ctx, http.MethodPatch, "/api/v1/folders/"+command.ID, nil, fields)
		case "folder.delete":
			body, err = s.Client.request(ctx, http.MethodDelete, "/api/v1/folders/"+command.ID, nil, fields)
		}
	case "secret.list":
		var names []SecretName
		names, err = s.Client.ListSecretNames(ctx, command.ProjectID, command.Environment, command.Path)
		if err == nil {
			for _, name := range names {
				result.Items = append(result.Items, Resource{Name: name.Name, Path: name.Path})
			}
		}
	case "secret.get", "secret.create", "secret.update", "secret.delete":
		endpoint := "/api/v4/secrets/" + command.Name
		query := url.Values{"projectId": {command.ProjectID}, "environment": {command.Environment}, "secretPath": {command.Path}, "viewSecretValue": {"false"}}
		fields := map[string]any{"projectId": command.ProjectID, "environment": command.Environment, "secretPath": command.Path}
		switch command.Operation {
		case "secret.get":
			body, err = s.Client.get(ctx, endpoint, query)
		case "secret.create":
			fields["secretValue"] = *command.Value
			body, err = s.Client.request(ctx, http.MethodPost, endpoint, nil, fields)
		case "secret.update":
			fields["secretValue"] = *command.Value
			body, err = s.Client.request(ctx, http.MethodPatch, endpoint, nil, fields)
		case "secret.delete":
			body, err = s.Client.request(ctx, http.MethodDelete, endpoint, nil, fields)
		}
	case "role.list", "role.create", "role.update", "role.delete":
		base := "/api/v1/projects/" + command.ProjectID + "/roles"
		fields := map[string]any{"slug": command.Slug, "name": command.Name, "description": command.Description, "permissions": command.Permissions}
		switch command.Operation {
		case "role.list":
			body, err = s.Client.get(ctx, base, nil)
		case "role.create":
			body, err = s.Client.request(ctx, http.MethodPost, base, nil, fields)
		case "role.update":
			body, err = s.Client.request(ctx, http.MethodPatch, base+"/"+command.ID, nil, fields)
		case "role.delete":
			body, err = s.Client.request(ctx, http.MethodDelete, base+"/"+command.ID, nil, nil)
		}
	case "membership.list", "membership.add", "membership.update", "membership.delete":
		base := "/api/v1/projects/" + command.ProjectID + "/identity-memberships"
		if command.Operation != "membership.list" {
			base += "/" + command.IdentityID
		}
		switch command.Operation {
		case "membership.list":
			body, err = s.Client.get(ctx, base, nil)
		case "membership.add", "membership.update":
			roles := make([]map[string]string, 0, len(command.Roles))
			for _, role := range command.Roles {
				roles = append(roles, map[string]string{"role": role})
			}
			method := http.MethodPost
			if command.Operation == "membership.update" {
				method = http.MethodPatch
			}
			body, err = s.Client.request(ctx, method, base, nil, map[string]any{"roles": roles})
		case "membership.delete":
			body, err = s.Client.request(ctx, http.MethodDelete, base, nil, nil)
		}
	}
	if err != nil {
		return CommandResult{}, err
	}
	if write {
		result.Applied = true
		if command.Operation == "project.create" {
			if created, decodeErr := readMetadata(body, "project.get"); decodeErr == nil {
				result.Items = created
			}
		}
		return result, nil
	}
	if command.Operation == "secret.list" {
		return result, nil
	}
	items, err := readMetadata(body, command.Operation)
	if err != nil {
		return CommandResult{}, err
	}
	if command.Operation == "project.list" {
		filtered := items[:0]
		for _, item := range items {
			if s.AllowedProjects[item.ID] {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	result.Items = items
	return result, nil
}

func validateCommand(c Command) (target string, write bool, err error) {
	parts := strings.Split(c.Operation, ".")
	if len(parts) != 2 {
		return "", false, ErrInvalidCommand
	}
	write = parts[1] != "list" && parts[1] != "get"
	if c.Operation != "project.create" && c.Operation != "project.list" && !resourceSegment.MatchString(c.ProjectID) {
		return "", false, ErrInvalidCommand
	}
	if c.Operation == "project.create" {
		if !resourceSegment.MatchString(c.Slug) || len(c.Slug) < 5 || len(c.Slug) > 36 || strings.TrimSpace(c.Name) == "" {
			return "", false, ErrInvalidCommand
		}
		return c.Slug, true, nil
	}
	target = c.ProjectID
	switch parts[0] {
	case "project":
		if parts[1] != "list" && parts[1] != "get" && parts[1] != "update" && parts[1] != "rename" && parts[1] != "delete" {
			return "", false, ErrInvalidCommand
		}
		if (parts[1] == "update" || parts[1] == "rename") && strings.TrimSpace(c.Name) == "" {
			return "", false, ErrInvalidCommand
		}
	case "environment":
		if parts[1] != "list" && parts[1] != "create" && parts[1] != "update" && parts[1] != "delete" {
			return "", false, ErrInvalidCommand
		}
		if parts[1] == "create" || parts[1] == "update" {
			if !resourceSegment.MatchString(c.Slug) || strings.TrimSpace(c.Name) == "" {
				return "", false, ErrInvalidCommand
			}
		}
		if parts[1] == "update" || parts[1] == "delete" {
			if !resourceSegment.MatchString(c.ID) {
				return "", false, ErrInvalidCommand
			}
			target += "/" + c.ID
		} else if parts[1] == "create" {
			target += "/" + c.Slug
		}
	case "folder", "secret":
		if parts[1] != "list" && parts[1] != "get" && parts[1] != "create" && parts[1] != "update" && parts[1] != "delete" {
			return "", false, ErrInvalidCommand
		}
		if parts[0] == "folder" && parts[1] == "get" {
			return "", false, ErrInvalidCommand
		}
		if !resourceSegment.MatchString(c.Environment) || !validCommandPath(c.Path) {
			return "", false, ErrInvalidCommand
		}
		target += "/" + c.Environment + c.Path
		if parts[0] == "folder" {
			if parts[1] == "create" || parts[1] == "update" {
				if !resourceSegment.MatchString(c.Name) {
					return "", false, ErrInvalidCommand
				}
			}
			if parts[1] == "update" || parts[1] == "delete" {
				if !resourceSegment.MatchString(c.ID) {
					return "", false, ErrInvalidCommand
				}
				target += "/" + c.ID
			} else if parts[1] == "create" {
				target += "/" + c.Name
			}
		} else if parts[1] != "list" {
			if !resourceSegment.MatchString(c.Name) {
				return "", false, ErrInvalidCommand
			}
			target += "/" + c.Name
			if (parts[1] == "create" || parts[1] == "update") && c.Value == nil {
				return "", false, ErrInvalidCommand
			}
		}
	case "role":
		if parts[1] != "list" && parts[1] != "create" && parts[1] != "update" && parts[1] != "delete" {
			return "", false, ErrInvalidCommand
		}
		if parts[1] == "create" {
			if !resourceSegment.MatchString(c.Slug) || strings.TrimSpace(c.Name) == "" {
				return "", false, ErrInvalidCommand
			}
			target += "/" + c.Slug
		}
		if parts[1] == "update" || parts[1] == "delete" {
			if !resourceSegment.MatchString(c.ID) {
				return "", false, ErrInvalidCommand
			}
			target += "/" + c.ID
		}
		if (parts[1] == "create" || parts[1] == "update") && (!resourceSegment.MatchString(c.Slug) || strings.TrimSpace(c.Name) == "" || !validPermissions(c.Permissions)) {
			return "", false, ErrInvalidCommand
		}
	case "membership":
		if parts[1] != "list" && parts[1] != "add" && parts[1] != "update" && parts[1] != "delete" {
			return "", false, ErrInvalidCommand
		}
		if parts[1] != "list" {
			if !resourceSegment.MatchString(c.IdentityID) {
				return "", false, ErrInvalidCommand
			}
			target += "/" + c.IdentityID
		}
		if (parts[1] == "add" || parts[1] == "update") && (len(c.Roles) == 0 || len(c.Roles) > 16) {
			return "", false, ErrInvalidCommand
		}
		for _, role := range c.Roles {
			if !resourceSegment.MatchString(role) {
				return "", false, ErrInvalidCommand
			}
		}
	default:
		return "", false, ErrInvalidCommand
	}
	return target, write, nil
}

func validCommandPath(path string) bool {
	if path == "/" {
		return true
	}
	if !strings.HasPrefix(path, "/") || len(path) > 256 || strings.HasSuffix(path, "/") {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if !resourceSegment.MatchString(part) || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validPermissions(rules []PermissionRule) bool {
	if len(rules) == 0 || len(rules) > 32 {
		return false
	}
	actions := map[string]bool{"read": true, "describeSecret": true, "readValue": true, "create": true, "edit": true, "delete": true}
	for _, rule := range rules {
		if rule.Subject != "secrets" || !actions[rule.Action] {
			return false
		}
	}
	return true
}

func readMetadata(body []byte, operation string) ([]Resource, error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return nil, errors.New("Infisical returned invalid metadata")
	}
	key := strings.Split(operation, ".")[0]
	if key == "environment" {
		key = "project"
	}
	var raw json.RawMessage
	if strings.HasSuffix(operation, ".list") && operation != "environment.list" {
		field := map[string]string{"project": "projects", "folder": "folders", "role": "roles", "membership": "identityMemberships"}[key]
		raw = envelope[field]
	} else {
		if key == "project" {
			raw = envelope["project"]
			if len(raw) == 0 {
				raw = body
			}
		} else {
			raw = envelope[key]
		}
	}
	if len(raw) == 0 {
		return nil, errors.New("Infisical returned missing metadata")
	}
	if operation == "environment.list" {
		var project map[string]json.RawMessage
		if json.Unmarshal(raw, &project) != nil {
			return nil, errors.New("Infisical returned invalid project metadata")
		}
		raw = project["environments"]
	}
	var entries []json.RawMessage
	if strings.HasSuffix(operation, ".list") {
		if json.Unmarshal(raw, &entries) != nil || len(entries) > 1000 {
			return nil, errors.New("Infisical returned invalid metadata list")
		}
	} else {
		entries = []json.RawMessage{raw}
	}
	items := make([]Resource, 0, len(entries))
	for _, entry := range entries {
		var item struct {
			ID          string          `json:"id"`
			Name        string          `json:"name"`
			Slug        string          `json:"slug"`
			Path        string          `json:"path"`
			IdentityID  string          `json:"identityId"`
			SecretKey   string          `json:"secretKey"`
			SecretPath  string          `json:"secretPath"`
			Environment json.RawMessage `json:"environment"`
			Roles       []struct {
				Role string `json:"role"`
			} `json:"roles"`
		}
		if json.Unmarshal(entry, &item) != nil {
			return nil, errors.New("Infisical returned invalid metadata item")
		}
		if item.Name == "" {
			item.Name = item.SecretKey
		}
		if item.Path == "" {
			item.Path = item.SecretPath
		}
		if operation == "membership.list" {
			item.Name = item.IdentityID
		}
		var environment string
		if len(item.Environment) > 0 {
			if item.Environment[0] == '"' {
				_ = json.Unmarshal(item.Environment, &environment)
			} else {
				var nested struct {
					Slug string `json:"slug"`
				}
				if json.Unmarshal(item.Environment, &nested) == nil {
					environment = nested.Slug
				}
			}
		}
		resource := Resource{ID: item.ID, Name: item.Name, Slug: item.Slug, Path: item.Path, Environment: environment}
		for _, role := range item.Roles {
			if role.Role != "" {
				resource.Roles = append(resource.Roles, role.Role)
			}
		}
		items = append(items, resource)
	}
	return items, nil
}

// ReadOperation is deliberately an allowlist: new operations are denied to K8.
func ReadOperation(operation string) bool {
	switch operation {
	case "project.list", "project.get", "environment.list", "folder.list", "secret.list", "secret.get":
		return true
	}
	return false
}
