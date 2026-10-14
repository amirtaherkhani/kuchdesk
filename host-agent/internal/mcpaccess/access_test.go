package mcpaccess

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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

type rejectedProjectSet struct{ status int }

func (p rejectedProjectSet) ListProjectIDs(context.Context) (map[string]bool, error) {
	return nil, &infisical.APIError{StatusCode: p.status}
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

func TestProjectCreationRequiresExplicitDevelopmentPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	p := accessPolicy()
	writeMCPPolicy(t, path, p)
	a, err := NewAuthorizer(path, "kubedesk", projectSet{"platform": true})
	if err != nil {
		t.Fatal(err)
	}
	command := infisical.Command{Operation: "project.create", Name: "dev-fixture", RequestID: "run-1"}
	if _, _, err := a.Authorize(context.Background(), command); !errors.Is(err, ErrDenied) {
		t.Fatal("creation was allowed without explicit policy")
	}
	ctx := p.MCPAccess.Contexts["kubedesk"]
	ctx.AllowProjectCreate = true
	ctx.ProjectNamePrefix = "dev-"
	p.MCPAccess.Contexts["kubedesk"] = ctx
	writeMCPPolicy(t, path, p)
	if _, _, err := a.Authorize(context.Background(), command); err != nil {
		t.Fatal("development creation was denied")
	}
	command.Name = "production"
	if _, _, err := a.Authorize(context.Background(), command); !errors.Is(err, ErrDenied) {
		t.Fatal("nondevelopment creation was allowed")
	}
}

func TestDisposableIdentityAdministrationRequiresExplicitPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	p := accessPolicy()
	writeMCPPolicy(t, path, p)
	a, err := NewAuthorizer(path, "kubedesk", projectSet{"platform": true})
	if err != nil {
		t.Fatal(err)
	}
	command := infisical.Command{Operation: "identity.create", Name: "codex-fixture"}
	if _, _, err := a.Authorize(context.Background(), command); !errors.Is(err, ErrDenied) {
		t.Fatal("identity creation was allowed without policy")
	}
	ctx := p.MCPAccess.Contexts["kubedesk"]
	ctx.AllowIdentityAdmin, ctx.IdentityNamePrefix = true, "codex-"
	p.MCPAccess.Contexts["kubedesk"] = ctx
	writeMCPPolicy(t, path, p)
	resolved, _, err := a.Authorize(context.Background(), command)
	if err != nil || resolved.OrganizationID != "org" {
		t.Fatal("identity authority did not bind the policy organization")
	}
	command.Name = "unrelated"
	if _, _, err := a.Authorize(context.Background(), command); !errors.Is(err, ErrDenied) {
		t.Fatal("unrelated identity creation was allowed")
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

func TestAuthorizerPreservesSanitizedUpstreamDenial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	writeMCPPolicy(t, path, accessPolicy())
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "authentication_rejected"},
		{http.StatusForbidden, "permission_denied"},
		{http.StatusNotFound, "not_found"},
	} {
		a, err := NewAuthorizer(path, "kubedesk", rejectedProjectSet{status: tc.status})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = a.Authorize(context.Background(), infisical.Command{Operation: "project.list"})
		if got := infisical.PublicError(err).Error(); got != tc.want {
			t.Fatalf("HTTP %d mapped to %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestScopedCommandsDenyBeforeUpstreamSideEffects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	writeMCPPolicy(t, path, accessPolicy())
	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			_, _ = io.WriteString(w, `{"accessToken":"fixture-token","expiresIn":3600,"tokenType":"Bearer"}`)
			return
		}
		upstreamCalls++
		if r.Method != http.MethodGet {
			t.Error("unexpected upstream write")
		}
		if r.URL.Path == "/api/v4/secrets" {
			_, _ = io.WriteString(w, `{"secrets":[]}`)
		} else {
			_, _ = io.WriteString(w, `{"secret":{"secretKey":"FIXTURE","secretValue":"redacted"}}`)
		}
	}))
	defer server.Close()
	client, err := infisical.NewClient(server.URL, "fixture-id", "fixture-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := NewAuthorizer(path, "kubedesk", projectSet{"platform": true, "readonly": true, "limited": true, "denied": true})
	if err != nil {
		t.Fatal(err)
	}
	service := infisical.CommandService{Client: client, MCPPolicy: authorizer}
	allowed := []infisical.Command{
		{Operation: "secret.get", ProjectID: "readonly", Environment: "dev", Path: "/app", Name: "FIXTURE"},
		{Operation: "secret.list", ProjectID: "limited", Environment: "dev", Path: "/app"},
	}
	for _, command := range allowed {
		if _, err := service.Execute(context.Background(), command); err != nil {
			t.Fatalf("allowed %s: %v", command.Operation, err)
		}
	}
	denied := []infisical.Command{
		{Operation: "secret.update", ProjectID: "readonly", Environment: "dev", Path: "/app", Name: "FIXTURE"},
		{Operation: "secret.list", ProjectID: "limited", Environment: "prod", Path: "/app"},
		{Operation: "secret.list", ProjectID: "limited", Environment: "dev", Path: "/application"},
		{Operation: "secret.get", ProjectID: "denied", Environment: "dev", Path: "/app", Name: "FIXTURE"},
		{Operation: "project.create", Name: "Test", Slug: "test-project"},
	}
	for _, command := range denied {
		if _, err := service.Execute(context.Background(), command); infisical.PublicError(err).Error() != "policy_denied" {
			t.Fatalf("%s on %s mapped to %v", command.Operation, command.ProjectID, err)
		}
	}
	if upstreamCalls != len(allowed) {
		t.Fatalf("denied command caused upstream call: %d", upstreamCalls)
	}
}

func TestSecretVerifierUsesCurrentPolicyWithoutSideEffectsOnDenial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	p := accessPolicy()
	p.MCPAccess.Projects["limited"] = enrollment.MCPProjectRule{Mode: enrollment.MCPModeLimited, Operations: []string{"secret.create", "secret.get", "secret.update", "secret.delete"}, Environments: []string{"dev"}, PathPrefixes: []string{"/app"}}
	writeMCPPolicy(t, path, p)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	client, err := infisical.NewClient(server.URL, "fixture-id", "fixture-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := NewAuthorizer(path, "kubedesk", projectSet{"readonly": true, "denied": true, "limited": true})
	if err != nil {
		t.Fatal(err)
	}
	service := infisical.CommandService{Client: client, MCPPolicy: authorizer}
	for _, scope := range []struct{ id, env, path string }{{"readonly", "dev", "/app"}, {"denied", "dev", "/app"}, {"limited", "prod", "/app"}, {"limited", "dev", "/application"}} {
		_, err := service.VerifySecretWrite(context.Background(), infisical.VerifyWriteInput{ProjectID: scope.id, Environment: scope.env, Path: scope.path, Confirm: "secret.verify_write:" + scope.id + "/" + scope.env + scope.path})
		if !errors.Is(err, ErrDenied) || calls != 0 {
			t.Fatal("fixture verifier bypassed current policy")
		}
	}
}
