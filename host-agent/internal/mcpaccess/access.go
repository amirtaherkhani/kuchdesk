// Package mcpaccess applies an optional, fail-closed MCP project policy.
package mcpaccess

import (
	"context"
	"errors"
	"strings"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/enrollment"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/infisical"
)

var (
	ErrDenied              = errors.New("MCP project policy denied command")
	ErrContextUnavailable  = errors.New("MCP client context is not configured")
	ErrPolicyUnavailable   = errors.New("MCP project policy is unavailable")
	ErrIdentityUnavailable = errors.New("Infisical identity project list is unavailable")
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
	if err != nil || visible == nil {
		return command, nil, ErrIdentityUnavailable
	}
	allowed := make(map[string]bool)
	for id, listed := range visible {
		if listed && p.Allows(id) && (clientContext.AllowCrossProject || id == clientContext.TargetProjectID) && ruleAllows(access.Projects[id], "project.list", "", "") {
			allowed[id] = true
		}
	}
	if command.Operation == "project.create" {
		return command, nil, ErrDenied
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
	if !allowed[command.ProjectID] || !supportedOperation(command.Operation) {
		return command, nil, ErrDenied
	}
	environment := command.Environment
	if command.Operation == "environment.create" {
		environment = command.Slug
	}
	if !ruleAllows(access.Projects[command.ProjectID], command.Operation, environment, command.Path) {
		return command, nil, ErrDenied
	}
	return command, allowed, nil
}

func supportedOperation(operation string) bool {
	switch operation {
	case "project.list", "project.get", "project.rename", "project.update", "project.delete",
		"environment.list", "environment.create", "environment.update", "environment.delete",
		"folder.list", "folder.create", "folder.update", "folder.delete",
		"secret.list", "secret.get", "secret.create", "secret.update", "secret.delete",
		"role.list", "role.create", "role.update", "role.delete",
		"membership.list", "membership.add", "membership.update", "membership.delete":
		return true
	default:
		return false
	}
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
		if !strings.HasSuffix(operation, ".list") && !strings.HasSuffix(operation, ".get") {
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
