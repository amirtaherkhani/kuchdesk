// Package mcpaccess applies an optional, fail-closed MCP project policy.
package mcpaccess

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/enrollment"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/infisical"
)

var (
	ErrDenied              = infisical.ErrPolicyDenied
	ErrContextUnavailable  = infisical.ErrContextUnavailable
	ErrPolicyUnavailable   = infisical.ErrPolicyUnavailable
	ErrIdentityUnavailable = infisical.ErrIdentityUnavailable
)

type ProjectLister interface {
	ListProjectIDs(context.Context) (map[string]bool, error)
}

type Authorizer struct {
	policyPath string
	contextKey string
	projects   ProjectLister
}

func NewAuthorizer(policyPath, contextKey string, projects ProjectLister) (*Authorizer, error) {
	if policyPath == "" || projects == nil {
		return nil, ErrPolicyUnavailable
	}
	if contextKey == "" {
		return nil, ErrContextUnavailable
	}
	p, err := enrollment.LoadPolicy(policyPath)
	if err != nil || p.MCPAccess == nil {
		return nil, ErrPolicyUnavailable
	}
	if _, ok := p.MCPAccess.Contexts[contextKey]; !ok {
		return nil, ErrContextUnavailable
	}
	return &Authorizer{policyPath: policyPath, contextKey: contextKey, projects: projects}, nil
}

func (a *Authorizer) Authorize(ctx context.Context, command infisical.Command) (infisical.Command, map[string]bool, error) {
	p, err := enrollment.LoadPolicy(a.policyPath)
	if err != nil || p.MCPAccess == nil {
		return command, nil, ErrPolicyUnavailable
	}
	access := p.MCPAccess
	clientContext, ok := access.Contexts[a.contextKey]
	if !ok {
		return command, nil, ErrContextUnavailable
	}
	visible, err := a.projects.ListProjectIDs(ctx)
	if err != nil {
		var apiErr *infisical.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden || apiErr.StatusCode == http.StatusNotFound) {
			return command, nil, err
		}
		if errors.Is(err, infisical.ErrInvalidResponse) {
			return command, nil, err
		}
		return command, nil, ErrIdentityUnavailable
	}
	if visible == nil {
		return command, nil, ErrIdentityUnavailable
	}
	allowed := make(map[string]bool)
	for id, listed := range visible {
		rule, configured := access.Projects[id]
		if len(access.Projects) > 0 && !configured {
			continue
		}
		if listed && p.Allows(id) && (clientContext.AllowCrossProject || id == clientContext.TargetProjectID) && ruleAllows(rule, "project.list", "", "") {
			allowed[id] = true
		}
	}
	if command.Operation == "project.create" {
		if !clientContext.AllowProjectCreate || !strings.HasPrefix(command.Name, clientContext.ProjectNamePrefix) || !p.AllProjects {
			return command, nil, ErrDenied
		}
		return command, allowed, nil
	}
	if command.Operation == "identity.create" || command.Operation == "identity.delete" {
		if !clientContext.AllowIdentityAdmin || !strings.HasPrefix(command.Name, clientContext.IdentityNamePrefix) || (command.IdentityID != "" && (command.IdentityID == p.HostIdentityID || command.IdentityID == p.K8IdentityID)) {
			return command, nil, ErrDenied
		}
		command.OrganizationID = p.OrganizationID
		return command, allowed, nil
	}
	if command.Operation == "project.list" {
		if len(allowed) == 0 {
			return command, nil, ErrDenied
		}
		return command, allowed, nil
	}
	if command.ProjectID == "" {
		command.ProjectID = clientContext.TargetProjectID
	}
	if !clientContext.AllowCrossProject && command.ProjectID != clientContext.TargetProjectID {
		return command, nil, ErrDenied
	}
	rule, configured := access.Projects[command.ProjectID]
	if len(access.Projects) > 0 && !configured {
		return command, nil, ErrDenied
	}
	if !allowed[command.ProjectID] || (!infisical.SupportsOperation(command.Operation) && command.Operation != "secret.read_value") {
		return command, nil, ErrDenied
	}
	environment := command.Environment
	if command.Operation == "environment.create" {
		environment = command.Slug
	}
	if !ruleAllows(rule, command.Operation, environment, command.Path) {
		return command, nil, ErrDenied
	}
	return command, allowed, nil
}

func ruleAllows(rule enrollment.MCPProjectRule, operation, environment, path string) bool {
	mode := rule.Mode
	if mode == "" {
		mode = enrollment.MCPModeIdentity
	}
	if operation == "project.list" {
		return mode != enrollment.MCPModeDeny
	}
	switch mode {
	case enrollment.MCPModeDeny:
		return false
	case enrollment.MCPModeReadOnly:
		if !strings.HasSuffix(operation, ".list") && !strings.HasSuffix(operation, ".get") && operation != "secret.read_value" {
			return false
		}
	case enrollment.MCPModeLimited:
		found := false
		for _, allowed := range rule.Operations {
			if operation == allowed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		if strings.HasPrefix(operation, "environment.") && (operation != "environment.create" || !contains(rule.Environments, environment)) {
			return false
		}
		if strings.HasPrefix(operation, "secret.") || strings.HasPrefix(operation, "folder.") {
			if !contains(rule.Environments, environment) || !pathMatches(rule.PathPrefixes, path) {
				return false
			}
		}
	case enrollment.MCPModeIdentity:
	default:
		return false
	}
	return true
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func pathMatches(prefixes []string, path string) bool {
	for _, prefix := range prefixes {
		if prefix == "/" || path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}
