package mcpaccess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/enrollment"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/infisical"
)

type projectSet map[string]bool

func (p projectSet) ListProjectIDs(context.Context) (map[string]bool, error) { return p, nil }

type failedProjectSet struct{}

func (failedProjectSet) ListProjectIDs(context.Context) (map[string]bool, error) {
	return nil, errors.New("upstream unavailable")
}

func writeMCPPolicy(t *testing.T, path string, p enrollment.Policy) {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func accessPolicy() enrollment.Policy {
	return enrollment.Policy{
		Version: "v0.151.0", OrganizationID: "org", HostIdentityID: "host", K8IdentityID: "k8",
		AllProjects: true, Exclude: []string{"revoked"}, IntervalSeconds: 30, MaxProjects: 10,
		MCPAccess: &enrollment.MCPAccessPolicy{
			Contexts: map[string]enrollment.MCPContext{"kubedesk": {TargetProjectID: "platform", AllowCrossProject: true}},
			Projects: map[string]enrollment.MCPProjectRule{
				"denied":   {Mode: enrollment.MCPModeDeny},
				"readonly": {Mode: enrollment.MCPModeReadOnly},
				"limited":  {Mode: enrollment.MCPModeLimited, Operations: []string{"secret.list", "environment.create"}, Environments: []string{"dev"}, PathPrefixes: []string{"/app"}},
			},
		},
	}
}

func TestAuthorizerResolvesTrustedContextAndIntersectsIdentityAndHostPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	writeMCPPolicy(t, path, accessPolicy())
	a, err := NewAuthorizer(path, "kubedesk", projectSet{"platform": true, "other": true, "revoked": true, "denied": true, "readonly": true, "limited": true})
	if err != nil {
		t.Fatal(err)
	}
	command, allowed, err := a.Authorize(context.Background(), infisical.Command{Operation: "secret.list", Environment: "dev", Path: "/app"})
	if err != nil || command.ProjectID != "platform" {
		t.Fatalf("resolved command = %+v, err=%v", command, err)
	}
	for _, id := range []string{"platform", "other", "readonly", "limited"} {
		if !allowed[id] {
			t.Errorf("expected allowed project %q in filtered list", id)
		}
	}
	for _, id := range []string{"revoked", "denied"} {
		if allowed[id] {
			t.Errorf("restricted project %q appeared in allowed list", id)
		}
	}
}

func TestAuthorizerRejectsUnmappedOrAmbiguousCrossProjectTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	p := accessPolicy()
	p.MCPAccess.Contexts["single"] = enrollment.MCPContext{TargetProjectID: "platform"}
	writeMCPPolicy(t, path, p)
	a, err := NewAuthorizer(path, "single", projectSet{"platform": true, "other": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Authorize(context.Background(), infisical.Command{Operation: "secret.list", ProjectID: "other", Environment: "dev", Path: "/app"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-project target error = %v", err)
	}
	if _, _, err := a.Authorize(context.Background(), infisical.Command{Operation: "secret.list", Environment: "dev", Path: "/app"}); err != nil {
		t.Fatalf("default target should resolve: %v", err)
	}
	if _, err := NewAuthorizer(path, "unknown-context", projectSet{"platform": true}); !errors.Is(err, ErrContextUnavailable) {
		t.Fatalf("unknown context error = %v", err)
	}
}

func TestAuthorizerAppliesProjectDenyReadOnlyAndLimitedScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	writeMCPPolicy(t, path, accessPolicy())
	projects := projectSet{"platform": true, "readonly": true, "limited": true, "denied": true}
	for _, tc := range []struct {
		id, operation, environment, secretPath string
		denied                                 bool
	}{
		{"denied", "secret.list", "dev", "/app", true},
		{"readonly", "secret.get", "dev", "/app", false},
		{"readonly", "secret.update", "dev", "/app", true},
		{"limited", "secret.list", "dev", "/app/child", false},
		{"limited", "secret.list", "prod", "/app", true},
		{"limited", "secret.list", "dev", "/application", true},
		{"limited", "secret.create", "dev", "/app", true},
	} {
		a, err := NewAuthorizer(path, "kubedesk", projects)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = a.Authorize(context.Background(), infisical.Command{Operation: tc.operation, ProjectID: tc.id, Environment: tc.environment, Path: tc.secretPath})
		if gotDenied := errors.Is(err, ErrDenied); gotDenied != tc.denied {
			t.Errorf("%s on %s %s%s: denied=%t err=%v", tc.operation, tc.id, tc.environment, tc.secretPath, gotDenied, err)
		}
	}
	for _, tc := range []struct {
		slug   string
		denied bool
	}{{"dev", false}, {"prod", true}} {
		a, err := NewAuthorizer(path, "kubedesk", projects)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = a.Authorize(context.Background(), infisical.Command{Operation: "environment.create", ProjectID: "limited", Slug: tc.slug, Name: tc.slug})
		if got := errors.Is(err, ErrDenied); got != tc.denied {
			t.Errorf("environment.create %q denied=%t err=%v", tc.slug, got, err)
		}
	}
}

func TestAuthorizerFailsClosedOnPolicyAndIdentityReadFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	writeMCPPolicy(t, path, accessPolicy())
	a, err := NewAuthorizer(path, "kubedesk", projectSet{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Authorize(context.Background(), infisical.Command{Operation: "secret.list", Environment: "dev", Path: "/app"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("unlisted identity project error = %v", err)
	}
	identityFailure, err := NewAuthorizer(path, "kubedesk", failedProjectSet{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := identityFailure.Authorize(context.Background(), infisical.Command{Operation: "secret.list", Environment: "dev", Path: "/app"}); !errors.Is(err, ErrIdentityUnavailable) {
		t.Fatalf("identity read failure error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"broken":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Authorize(context.Background(), infisical.Command{Operation: "secret.list", Environment: "dev", Path: "/app"}); !errors.Is(err, ErrPolicyUnavailable) {
		t.Fatalf("invalid policy error = %v", err)
	}
}
