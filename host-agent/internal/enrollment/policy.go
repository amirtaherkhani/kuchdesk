// Package enrollment implements the local-only Infisical enrollment control plane.
package enrollment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var ErrDenied = errors.New("denied")
var ErrEnrollment = errors.New("enrollment_required")
var ErrExpired = errors.New("session_expired")
var ErrUnsupported = errors.New("unsupported_version")
var ErrUnavailable = errors.New("temporarily_unavailable")

// Policy is local administrator intent, not authority inferred from discovery.
// Changing organization, identity or version requires restarting the controller.
type Policy struct {
	Version           string           `json:"version"`
	OrganizationID    string           `json:"organizationId"`
	HostIdentityID    string           `json:"hostIdentityId"`
	K8IdentityID      string           `json:"k8IdentityId,omitempty"`
	K8IdentityName    string           `json:"k8IdentityName,omitempty"`
	CreateK8Identity  bool             `json:"createK8Identity"`
	Include           []string         `json:"include"`
	Exclude           []string         `json:"exclude"`
	AllProjects       bool             `json:"allProjects"`
	Apply             bool             `json:"apply"`
	RepairRevocations bool             `json:"repairRevocations"`
	IntervalSeconds   int              `json:"intervalSeconds"`
	MaxProjects       int              `json:"maxProjects"`
	MCPAccess         *MCPAccessPolicy `json:"mcpAccess,omitempty"`
}

// MCPAccessPolicy is an optional, local administrator policy for the MCP
// management surface. It never grants upstream Infisical permissions.
type MCPAccessPolicy struct {
	Contexts map[string]MCPContext     `json:"contexts"`
	Projects map[string]MCPProjectRule `json:"projects,omitempty"`
}

type MCPContext struct {
	TargetProjectID    string `json:"targetProjectId"`
	AllowCrossProject  bool   `json:"allowCrossProject,omitempty"`
	AllowProjectCreate bool   `json:"allowProjectCreate,omitempty"`
	ProjectNamePrefix  string `json:"projectNamePrefix,omitempty"`
	AllowIdentityAdmin bool   `json:"allowIdentityAdmin,omitempty"`
	IdentityNamePrefix string `json:"identityNamePrefix,omitempty"`
}

type MCPProjectRule struct {
	Mode         MCPMode  `json:"mode,omitempty"`
	Operations   []string `json:"operations,omitempty"`
	Environments []string `json:"environments,omitempty"`
	PathPrefixes []string `json:"pathPrefixes,omitempty"`
}

type MCPMode string

const (
	MCPModeIdentity MCPMode = "identity"
	MCPModeDeny     MCPMode = "deny"
	MCPModeReadOnly MCPMode = "read-only"
	MCPModeLimited  MCPMode = "limited"
)

var mcpOperations = map[string]bool{
	"project.list": true, "project.get": true, "project.rename": true, "project.update": true, "project.delete": true,
	"environment.list": true, "environment.create": true, "environment.update": true, "environment.delete": true,
	"folder.list": true, "folder.create": true, "folder.update": true, "folder.delete": true,
	"secret.list": true, "secret.get": true, "secret.create": true, "secret.update": true, "secret.delete": true,
	"secret.read_value": true,
	"role.list":         true, "role.create": true, "role.update": true, "role.delete": true,
	"membership.list": true, "membership.add": true, "membership.update": true, "membership.delete": true,
}

func (m MCPMode) Valid() bool {
	return m == "" || m == MCPModeIdentity || m == MCPModeDeny || m == MCPModeReadOnly || m == MCPModeLimited
}

func (p MCPAccessPolicy) Validate() error {
	if len(p.Contexts) == 0 {
		return errors.New("invalid_mcp_context_policy")
	}
	for key, ctx := range p.Contexts {
		if !identifier.MatchString(key) || !identifier.MatchString(ctx.TargetProjectID) {
			return errors.New("invalid_mcp_context_policy")
		}
		if ctx.AllowProjectCreate && (!ctx.AllowCrossProject || len(ctx.ProjectNamePrefix) < 4 || len(ctx.ProjectNamePrefix) > 32 || !identifier.MatchString(ctx.ProjectNamePrefix)) {
			return errors.New("invalid_mcp_context_policy")
		}
		if !ctx.AllowProjectCreate && ctx.ProjectNamePrefix != "" {
			return errors.New("invalid_mcp_context_policy")
		}
		if ctx.AllowIdentityAdmin && (len(ctx.IdentityNamePrefix) < 4 || len(ctx.IdentityNamePrefix) > 32 || !identifier.MatchString(ctx.IdentityNamePrefix)) {
			return errors.New("invalid_mcp_context_policy")
		}
		if !ctx.AllowIdentityAdmin && ctx.IdentityNamePrefix != "" {
			return errors.New("invalid_mcp_context_policy")
		}
	}
	for id, rule := range p.Projects {
		if !identifier.MatchString(id) || !rule.Mode.Valid() {
			return errors.New("invalid_mcp_project_policy")
		}
		for _, op := range rule.Operations {
			if !mcpOperations[op] {
				return errors.New("invalid_mcp_project_policy")
			}
		}
		if rule.Mode == MCPModeDeny && (len(rule.Operations) != 0 || len(rule.Environments) != 0 || len(rule.PathPrefixes) != 0) {
			return errors.New("invalid_mcp_project_policy")
		}
		if rule.Mode != MCPModeLimited && rule.Mode != MCPModeDeny && (len(rule.Operations) != 0 || len(rule.Environments) != 0 || len(rule.PathPrefixes) != 0) {
			return errors.New("invalid_mcp_project_policy")
		}
		if rule.Mode == MCPModeLimited && len(rule.Operations) == 0 {
			return errors.New("invalid_mcp_project_policy")
		}
		if rule.Mode == MCPModeLimited {
			for _, op := range rule.Operations {
				if op == "environment.list" || op == "environment.update" || op == "environment.delete" {
					return errors.New("invalid_mcp_project_policy")
				}
				if op == "environment.create" && len(rule.Environments) == 0 {
					return errors.New("invalid_mcp_project_policy")
				}
			}
		}
		for _, env := range rule.Environments {
			if !identifier.MatchString(env) {
				return errors.New("invalid_mcp_project_policy")
			}
		}
		for _, prefix := range rule.PathPrefixes {
			if !strings.HasPrefix(prefix, "/") || strings.Contains(prefix, "//") || (prefix != "/" && strings.HasSuffix(prefix, "/")) {
				return errors.New("invalid_mcp_project_policy")
			}
			for _, part := range strings.Split(strings.TrimPrefix(prefix, "/"), "/") {
				if part != "" && (!identifier.MatchString(part) || part == "." || part == "..") {
					return errors.New("invalid_mcp_project_policy")
				}
			}
		}
		for _, op := range rule.Operations {
			if strings.HasPrefix(op, "secret.") || strings.HasPrefix(op, "folder.") {
				if len(rule.Environments) == 0 || len(rule.PathPrefixes) == 0 {
					return errors.New("invalid_mcp_project_policy")
				}
			}
		}
	}
	return nil
}

func (p Policy) Validate() error {
	if p.Version != "v0.151.0" && p.Version != "v0.166.3" {
		return ErrUnsupported
	}
	if !identifier.MatchString(p.OrganizationID) || !identifier.MatchString(p.HostIdentityID) || (p.K8IdentityID != "" && !identifier.MatchString(p.K8IdentityID)) || p.K8IdentityID == p.HostIdentityID {
		return errors.New("invalid_identity_policy")
	}
	if p.K8IdentityID == "" && !identifier.MatchString(p.K8IdentityName) {
		return errors.New("k8_identity_required")
	}
	if p.IntervalSeconds < 30 || p.IntervalSeconds > 3600 || p.MaxProjects < 1 || p.MaxProjects > 1000 {
		return errors.New("invalid_reconciliation_bounds")
	}
	for _, ids := range [][]string{p.Include, p.Exclude} {
		for _, id := range ids {
			if !identifier.MatchString(id) {
				return errors.New("invalid_project_policy")
			}
		}
	}
	if p.MCPAccess != nil {
		if err := p.MCPAccess.Validate(); err != nil {
			return err
		}
	}
	return nil
}
func (p Policy) Allows(id string) bool {
	for _, v := range p.Exclude {
		if v == id {
			return false
		}
	}
	if p.AllProjects {
		return true
	}
	for _, v := range p.Include {
		if v == id {
			return true
		}
	}
	return false
}
func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid_local_state")
	}
	return nil
}
func LoadPolicy(path string) (Policy, error) {
	var p Policy
	b, err := readPrivate(path)
	if err != nil {
		return p, err
	}
	if err = decode(b, &p); err != nil {
		return p, err
	}
	return p, p.Validate()
}

// Access publishes only currently verified projects. Invalid policy clears access.
// Requests re-read policy, so excludes/invalid edits take effect without a restart.
type Access struct {
	Binding  Policy
	Path     string
	mu       sync.RWMutex
	verified map[string]bool
}

func (a *Access) Publish(ids map[string]bool) { a.mu.Lock(); defer a.mu.Unlock(); a.verified = ids }

// Withdraw immediately removes eligibility while this project's authority is checked.
func (a *Access) Withdraw(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.verified, id)
}

func (a *Access) Projects() map[string]bool {
	p, e := LoadPolicy(a.Path)
	if e != nil || !sameBinding(p, a.Binding) {
		return map[string]bool{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := map[string]bool{}
	for id, ok := range a.verified {
		if ok && p.Allows(id) {
			out[id] = true
		}
	}
	return out
}
func delay(p Policy, failures int) time.Duration {
	d := time.Duration(p.IntervalSeconds) * time.Second
	if d < 30*time.Second {
		d = 30 * time.Second
	}
	for i := 0; i < failures && d < 15*time.Minute; i++ {
		d *= 2
	}
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}
