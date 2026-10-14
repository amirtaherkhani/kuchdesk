package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var resourceSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var ErrInvalidCommand = errors.New("invalid Infisical command")
var ErrProjectDenied = errors.New("Infisical project is not in the host allowlist")
var ErrConfirmation = errors.New("exact Infisical command confirmation required")

var supportedOperations = []string{
	"project.list", "project.get", "project.create", "project.rename", "project.update", "project.delete",
	"environment.list", "environment.create", "environment.update", "environment.delete",
	"folder.list", "folder.create", "folder.update", "folder.delete",
	"secret.list", "secret.get", "secret.create", "secret.update", "secret.delete",
	"role.list", "role.create", "role.update", "role.delete",
	"membership.list", "membership.add", "membership.update", "membership.delete",
	"identity.create", "identity.delete",
}

func SupportedOperations() []string { return append([]string(nil), supportedOperations...) }

func SupportsOperation(operation string) bool {
	for _, supported := range supportedOperations {
		if operation == supported {
			return true
		}
	}
	return false
}

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
	Operation         string           `json:"operation"`
	ProjectID         string           `json:"projectId,omitempty"`
	ID                string           `json:"id,omitempty"`
	IdentityID        string           `json:"identityId,omitempty"`
	OrganizationID    string           `json:"-"`
	Environment       string           `json:"environment,omitempty"`
	Path              string           `json:"path,omitempty"`
	Name              string           `json:"name,omitempty"`
	Slug              string           `json:"slug,omitempty"`
	Description       string           `json:"description,omitempty"`
	Type              string           `json:"type,omitempty"`
	Template          string           `json:"template,omitempty"`
	CreateDefaultEnvs *bool            `json:"shouldCreateDefaultEnvs,omitempty"`
	RequestID         string           `json:"requestId,omitempty"`
	Value             *string          `json:"value,omitempty"`
	Permissions       []PermissionRule `json:"permissions,omitempty"`
	Roles             []string         `json:"roles,omitempty"`
	Confirm           string           `json:"confirm,omitempty"`
}

type PermissionRule struct {
	Subject string `json:"subject"`
	Action  string `json:"action"`
}

// Resource is deliberately value-free, even for a GET secret response.
type Resource struct {
	ID           string                `json:"id,omitempty"`
	Name         string                `json:"name,omitempty"`
	Slug         string                `json:"slug,omitempty"`
	Description  string                `json:"description,omitempty"`
	Type         string                `json:"type,omitempty"`
	Environments []EnvironmentMetadata `json:"environments,omitempty"`
	Path         string                `json:"path,omitempty"`
	Environment  string                `json:"environment,omitempty"`
	Roles        []string              `json:"roles,omitempty"`
}

type EnvironmentMetadata struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type CommandResult struct {
	Operation        string     `json:"operation"`
	Target           string     `json:"target"`
	Applied          bool       `json:"applied,omitempty"`
	Items            []Resource `json:"items,omitempty"`
	EnrollmentStatus string     `json:"enrollmentStatus,omitempty"`
}

// ValueResult is deliberately separate from CommandResult. Callers must opt in
// to placing secret values in their response context.
type ValueResult struct {
	ProjectID   string      `json:"projectId"`
	Environment string      `json:"environment"`
	Path        string      `json:"path"`
	Items       []ValueItem `json:"items"`
	NextOffset  *int        `json:"nextOffset,omitempty"`
}

type ValueItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ValueInput struct {
	ProjectID   string `json:"projectId" jsonschema:"required,exact Infisical project ID"`
	Environment string `json:"environment" jsonschema:"required,actual environment slug"`
	Path        string `json:"path" jsonschema:"required,absolute exact folder path"`
	Name        string `json:"name,omitempty"`
	Offset      int    `json:"offset,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// ReadValues reads only the exact path. Imports, recursion and interpolation
// are disabled so an allowed path cannot pull values from another scope.
func (s CommandService) ReadValues(ctx context.Context, input ValueInput) (ValueResult, error) {
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 500 || (input.Name != "" && (input.Offset != 0 || input.Limit != 0)) {
		return ValueResult{}, ErrInvalidCommand
	}
	command := Command{Operation: "secret.read_value", ProjectID: input.ProjectID, Environment: input.Environment, Path: input.Path, Name: input.Name}
	s, command, _, _, err := s.authorizeCommand(ctx, command)
	if err != nil {
		return ValueResult{}, err
	}
	result := ValueResult{ProjectID: command.ProjectID, Environment: command.Environment, Path: command.Path, Items: []ValueItem{}}
	query := url.Values{"projectId": {command.ProjectID}, "environment": {command.Environment}, "secretPath": {command.Path}, "viewSecretValue": {"true"}, "recursive": {"false"}, "include_imports": {"false"}, "expandSecretReferences": {"false"}}
	endpoint := "/api/v4/secrets"
	if command.Name != "" {
		endpoint += "/" + command.Name
	}
	body, err := s.Client.get(ctx, endpoint, query)
	if err != nil {
		return ValueResult{}, err
	}
	var envelope struct {
		Secret  json.RawMessage   `json:"secret"`
		Secrets []json.RawMessage `json:"secrets"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ValueResult{}, ErrInvalidResponse
	}
	entries := envelope.Secrets
	if command.Name != "" {
		entries = []json.RawMessage{envelope.Secret}
	}
	if len(entries) > 1000 || (command.Name != "" && len(envelope.Secret) == 0) || (command.Name == "" && envelope.Secrets == nil) {
		return ValueResult{}, ErrInvalidResponse
	}
	seen := make(map[string]bool, len(entries))
	for _, raw := range entries {
		var item struct {
			Name   string  `json:"secretKey"`
			Value  *string `json:"secretValue"`
			Path   string  `json:"secretPath"`
			Hidden bool    `json:"secretValueHidden"`
		}
		if json.Unmarshal(raw, &item) != nil || !resourceSegment.MatchString(item.Name) || item.Value == nil || item.Hidden || (command.Name != "" && item.Name != command.Name) || (item.Path != "" && item.Path != command.Path) || seen[item.Name] {
			return ValueResult{}, ErrInvalidResponse
		}
		seen[item.Name] = true
		result.Items = append(result.Items, ValueItem{Name: item.Name, Value: *item.Value})
	}
	if command.Name == "" {
		sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
		limit := input.Limit
		if limit == 0 {
			limit = 100
		}
		if input.Offset > len(result.Items) {
			return ValueResult{}, ErrInvalidCommand
		}
		end := input.Offset + limit
		if end < len(result.Items) {
			result.NextOffset = &end
		} else {
			end = len(result.Items)
		}
		result.Items = result.Items[input.Offset:end]
	}
	return result, nil
}

// CommandService is the policy boundary before the version-pinned Infisical
// adapter. Its allowlist is separate from the server's bearer authentication.
type CommandService struct {
	Client             *Client
	AllowedProjects    map[string]bool
	AllowProjectCreate bool
	AllowIdentityAdmin bool
	ProvisionDir       string
	ProjectPolicy      interface{ Projects() map[string]bool }
	MCPPolicy          CommandAuthorizer
	ReadOnly           bool
}

func (s CommandService) Execute(ctx context.Context, command Command) (CommandResult, error) {
	if command.Operation == "secret.read_value" {
		return CommandResult{}, ErrInvalidCommand
	}
	var target string
	var write bool
	var err error
	s, command, target, write, err = s.authorizeCommand(ctx, command)
	if err != nil {
		return CommandResult{}, err
	}
	result := CommandResult{Operation: command.Operation, Target: target}
	var body []byte
	switch command.Operation {
	case "project.list":
		body, err = s.Client.get(ctx, "/api/v1/projects", nil)
	case "project.get", "environment.list":
		body, err = s.Client.get(ctx, "/api/v1/projects/"+command.ProjectID, nil)
	case "project.create":
		return s.createProject(ctx, command, result)
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
	case "identity.create":
		return s.createIdentity(ctx, command, result)
	case "identity.delete":
		return s.deleteIdentity(ctx, command, result)
	}
	if err != nil {
		return CommandResult{}, err
	}
	if write {
		result.Applied = true
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

// Authorize checks a command without executing it. Async MCP operations use it
// before accepting work and reauthorize again when the work runs.
func (s CommandService) Authorize(ctx context.Context, command Command) error {
	_, _, _, _, err := s.authorizeCommand(ctx, command)
	return err
}

// authorizeCommand is shared by ordinary commands and the fixture verifier.
// It resolves current policy on every request without performing a mutation.
func (s CommandService) authorizeCommand(ctx context.Context, command Command) (authorized CommandService, resolved Command, target string, write bool, err error) {
	if s.Client == nil {
		return s, command, "", false, ErrNotConfigured
	}
	if s.MCPPolicy != nil {
		resolved, allowed, err := s.MCPPolicy.Authorize(ctx, command)
		if err != nil {
			return s, command, "", false, err
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
	}
	if s.ReadOnly && !ReadOperation(command.Operation) {
		return s, command, "", false, ErrProjectDenied
	}
	if s.ProjectPolicy != nil && s.MCPPolicy == nil {
		s.AllowedProjects = s.ProjectPolicy.Projects()
	}
	target, write, err = validateCommand(command)
	if err != nil {
		return s, command, "", false, err
	}
	if command.Operation == "project.create" {
		if !s.AllowProjectCreate {
			return s, command, "", false, ErrProjectDenied
		}
	} else if command.Operation == "identity.create" || command.Operation == "identity.delete" {
		if !s.AllowIdentityAdmin || s.MCPPolicy == nil {
			return s, command, "", false, ErrProjectDenied
		}
	} else if command.Operation == "project.list" {
		if len(s.AllowedProjects) == 0 {
			return s, command, "", false, ErrProjectDenied
		}
	} else if !s.AllowedProjects[command.ProjectID] {
		return s, command, "", false, ErrProjectDenied
	}
	if write && command.Confirm != command.Operation+":"+target {
		return s, command, "", false, ErrConfirmation
	}
	return s, command, target, write, nil
}

func validateCommand(c Command) (target string, write bool, err error) {
	parts := strings.Split(c.Operation, ".")
	if len(parts) != 2 {
		return "", false, ErrInvalidCommand
	}
	write = parts[1] != "list" && parts[1] != "get" && c.Operation != "secret.read_value"
	if c.Operation != "project.create" && c.Operation != "project.list" && c.Operation != "identity.create" && c.Operation != "identity.delete" && !resourceSegment.MatchString(c.ProjectID) {
		return "", false, ErrInvalidCommand
	}
	if c.Operation == "project.create" {
		if (c.Slug != "" && (!resourceSegment.MatchString(c.Slug) || len(c.Slug) < 5 || len(c.Slug) > 36)) || !validProjectText(c.Name, 128, true) || !validProjectText(c.Description, 256, false) || (c.Type != "" && c.Type != "secret-manager") || (c.Template != "" && (!resourceSegment.MatchString(c.Template) || len(c.Template) > 64)) || (c.RequestID != "" && !resourceSegment.MatchString(c.RequestID)) {
			return "", false, ErrInvalidCommand
		}
		if c.RequestID != "" {
			return c.RequestID, true, nil
		}
		return c.Slug, true, nil
	}
	if c.Operation == "identity.create" || c.Operation == "identity.delete" {
		if !resourceSegment.MatchString(c.Name) || (c.Operation == "identity.delete" && !resourceSegment.MatchString(c.IdentityID)) || !resourceSegment.MatchString(c.OrganizationID) {
			return "", false, ErrInvalidCommand
		}
		if c.Operation == "identity.create" {
			return c.Name, true, nil
		}
		return c.IdentityID, true, nil
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
		if parts[1] != "list" && parts[1] != "get" && parts[1] != "create" && parts[1] != "update" && parts[1] != "delete" && !(parts[0] == "secret" && parts[1] == "read_value") {
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
		} else if parts[1] != "list" && parts[1] != "read_value" {
			if !resourceSegment.MatchString(c.Name) {
				return "", false, ErrInvalidCommand
			}
			target += "/" + c.Name
			if (parts[1] == "create" || parts[1] == "update") && c.Value == nil {
				return "", false, ErrInvalidCommand
			}
		}
		if parts[0] == "secret" && parts[1] == "read_value" && c.Name != "" && !resourceSegment.MatchString(c.Name) {
			return "", false, ErrInvalidCommand
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

func validProjectText(value string, max int, required bool) bool {
	if len(value) > max || (required && strings.TrimSpace(value) == "") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
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
		return nil, ErrInvalidResponse
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
		return nil, ErrInvalidResponse
	}
	if operation == "environment.list" {
		var project map[string]json.RawMessage
		if json.Unmarshal(raw, &project) != nil {
			return nil, ErrInvalidResponse
		}
		raw = project["environments"]
	}
	var entries []json.RawMessage
	if strings.HasSuffix(operation, ".list") {
		if json.Unmarshal(raw, &entries) != nil || len(entries) > 1000 {
			return nil, ErrInvalidResponse
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
			return nil, ErrInvalidResponse
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
