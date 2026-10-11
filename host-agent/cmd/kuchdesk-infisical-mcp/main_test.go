package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/async"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/deploy"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/infisical"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpHealthyPreflight struct{}

func (mcpHealthyPreflight) Run(context.Context, doctor.Config) (doctor.Report, error) {
	checks := make([]doctor.Check, 7)
	for i := range checks {
		checks[i].Status = "ok"
	}
	return doctor.Report{Healthy: true, Checks: checks}, nil
}

func TestMCPDiscoveryAndRedactedErrors(t *testing.T) {
	client, err := infisical.NewClient("https://infisical.example", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(client).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test-harness"}, nil)
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 13 {
		t.Fatalf("expected thirteen MCP tools, got %+v", tools.Tools)
	}
	for _, tool := range tools.Tools {
		if tool.InputSchema == nil {
			t.Fatalf("tool %s has no input schema", tool.Name)
		}
	}
	capabilities, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "infisical_capabilities", Arguments: map[string]any{}})
	if err != nil || capabilities.IsError {
		t.Fatalf("capability discovery failed: %v %+v", err, capabilities)
	}
	encoded, _ := json.Marshal(capabilities.StructuredContent)
	if !strings.Contains(string(encoded), `"configured":false`) {
		t.Fatalf("unexpected capability output: %s", encoded)
	}
	invalidDoctor, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_doctor", Arguments: map[string]any{"domain": "infisical.local.dev", "kubeContext": "docker-desktop", "registryUrl": "https://example.com", "diskPath": "/tmp"}})
	if err != nil || !invalidDoctor.IsError {
		t.Fatalf("unsafe Doctor input accepted: %v %+v", err, invalidDoctor)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "infisical_list_secret_names", Arguments: map[string]any{
		"projectId": "project", "environment": "dev", "secretPath": "/",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("missing credentials should return a tool error")
	}
	encoded, _ = json.Marshal(result)
	if !strings.Contains(string(encoded), "credentials_not_configured") || strings.Contains(string(encoded), "clientSecret") {
		t.Fatalf("unexpected error output: %s", encoded)
	}
}

func TestDoctorScopesExposeOnlySupportedToolsWithoutInfisicalCredentials(t *testing.T) {
	for _, tc := range []struct {
		scope string
		want  []string
	}{
		{"doctor-readonly", []string{"kuchdesk_doctor", "kuchdesk_doctor_verify"}},
		{"doctor-repair", []string{"kuchdesk_deploy_plan", "kuchdesk_deploy_preflight", "kuchdesk_deploy_start", "kuchdesk_job_status", "kuchdesk_cancel_job", "kuchdesk_doctor", "kuchdesk_doctor_validate_plan", "kuchdesk_doctor_verify"}},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			ctx := context.Background()
			runner := async.NewRunner(ctx, 2, 4)
			defer runner.Close()
			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			serverSession, err := newServerWithScope(nil, runner, deployConfig{}, fakeDoctorRunner{doctor.Report{SchemaVersion: "kuchdesk.doctor/v2", Healthy: true}}, tc.scope).Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer serverSession.Close()
			client, err := mcp.NewClient(&mcp.Implementation{Name: "scope-test"}, nil).Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			catalog, err := client.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.Tools) != len(tc.want) {
				t.Fatalf("scope=%s exposed %d tools, want %d", tc.scope, len(catalog.Tools), len(tc.want))
			}
			expected := make(map[string]bool, len(tc.want))
			for _, name := range tc.want {
				expected[name] = true
			}
			for _, tool := range catalog.Tools {
				if !expected[tool.Name] || tool.InputSchema == nil {
					t.Fatalf("scope=%s exposed unsupported or untyped tool %s", tc.scope, tool.Name)
				}
			}
			prompts, err := client.ListPrompts(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.scope == "doctor-readonly" && len(prompts.Prompts) != 0 || tc.scope == "doctor-repair" && (len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "kuchdesk_doctor_repair") {
				t.Fatalf("scope=%s prompts=%+v", tc.scope, prompts.Prompts)
			}
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_doctor", Arguments: map[string]any{"domain": "infisical.local.dev", "kubeContext": "docker-desktop", "registryUrl": "http://127.0.0.1:5001", "diskPath": "/tmp"}})
			if err != nil || result.IsError {
				t.Fatalf("Doctor unavailable in %s: %v %+v", tc.scope, err, result)
			}
			if tc.scope == "doctor-repair" {
				start, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_deploy_start", Arguments: map[string]any{"profile": "agent.json", "confirm": "kuchdesk-agent/development-tools"}})
				if err != nil || !start.IsError {
					t.Fatalf("disabled repair accepted: %v %+v", err, start)
				}
			}
		})
	}
}

func TestInfisicalManagementToolIsExplicitAndProjectScoped(t *testing.T) {
	client, err := infisical.NewClient("https://infisical.example", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runner := async.NewRunner(ctx, 2, 4)
	defer runner.Close()
	server := newServerWithScope(infisical.NewService(client), runner, deployConfig{}, fakeDoctorRunner{}, "all")
	registerInfisicalManagement(server, infisical.CommandService{Client: client, AllowedProjects: map[string]bool{"allowed": true}})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "management-test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 14 {
		t.Fatalf("management tool catalog = %d, %v", len(tools.Tools), err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "infisical_manage", Arguments: map[string]any{"operation": "secret.get", "projectId": "denied", "environment": "dev", "path": "/", "name": "API_KEY"}})
	if err != nil || !result.IsError {
		t.Fatalf("project allowlist bypassed: %v %+v", err, result)
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), "project_denied") || strings.Contains(string(encoded), "clientSecret") {
		t.Fatalf("unredacted management error: %s", encoded)
	}
}

type fakeDoctorRunner struct{ report doctor.Report }

func (f fakeDoctorRunner) Run(context.Context, doctor.Config) (doctor.Report, error) {
	return f.report, nil
}

func TestDoctorMCPPromptSchemaCatalogAndFailedVerification(t *testing.T) {
	ctx := context.Background()
	runner := async.NewRunner(ctx, 2, 4)
	defer runner.Close()
	check := doctor.Check{ID: "dns", Component: "host-dns", Status: "fail", Message: "Configured domain does not resolve", Source: "system-resolver", Context: "macos", ObservedAt: time.Now().UTC(), ErrorClass: "dns_lookup_failed", Truncated: false}
	fake := fakeDoctorRunner{doctor.Report{SchemaVersion: "kuchdesk.doctor/v2", GeneratedAt: time.Now().UTC(), Healthy: false, Summary: doctor.Summary{Total: 1, Failed: 1}, Checks: []doctor.Check{check}, Findings: []doctor.Finding{{CheckID: "dns", Severity: "fail", Problem: check.Message, Recommendation: "Check DNS"}}, Limitations: []string{"Point-in-time"}}}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithServices(infisical.NewService(fakeNameLister{}), runner, deployConfig{}, fake).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "doctor-test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	tools, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]bool)
	for _, tool := range tools.Tools {
		known[tool.Name] = true
	}
	for _, capability := range doctor.ToolCatalog() {
		if !known[capability.Name] {
			t.Fatalf("catalog lists unsupported tool %s", capability.Name)
		}
	}
	prompts, err := client.ListPrompts(ctx, nil)
	if err != nil || len(prompts.Prompts) != 1 || prompts.Prompts[0].Name != "kuchdesk_doctor_repair" {
		t.Fatalf("prompt discovery failed: %+v %v", prompts, err)
	}
	arguments := map[string]string{"domain": "infisical.local.dev", "kubeContext": "docker-desktop", "registryUrl": "http://127.0.0.1:5001", "diskPath": t.TempDir()}
	prompt, err := client.GetPrompt(ctx, &mcp.GetPromptParams{Name: "kuchdesk_doctor_repair", Arguments: arguments})
	if err != nil || len(prompt.Messages) != 1 {
		t.Fatalf("prompt retrieval failed: %+v %v", prompt, err)
	}
	encoded, _ := json.Marshal(prompt)
	if !strings.Contains(string(encoded), "dns_lookup_failed") || !strings.Contains(string(encoded), "kuchdesk_deploy_start") || strings.Contains(string(encoded), "provider_not_configured") {
		t.Fatalf("wrong MCP prompt content: %s", encoded)
	}
	schema, err := client.ReadResource(ctx, &mcp.ReadResourceParams{URI: "kuchdesk://doctor/report/v2"})
	if err != nil || len(schema.Contents) != 1 || !strings.Contains(schema.Contents[0].Text, "kuchdesk.doctor/v2") {
		t.Fatalf("schema resource unavailable: %+v %v", schema, err)
	}
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_doctor_verify", Arguments: map[string]any{"domain": arguments["domain"], "kubeContext": arguments["kubeContext"], "registryUrl": arguments["registryUrl"], "diskPath": arguments["diskPath"], "checkIds": []string{"dns"}}})
	if err != nil || result.IsError {
		t.Fatalf("verification tool failed: %+v %v", result, err)
	}
	encoded, _ = json.Marshal(result.StructuredContent)
	if !strings.Contains(string(encoded), `"success":false`) {
		t.Fatalf("failed recheck marked repaired: %s", encoded)
	}
	unsupported, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_doctor_validate_plan", Arguments: map[string]any{"steps": []map[string]any{{"tool": "shell_exec", "profile": "agent.json"}}}})
	if err != nil || !unsupported.IsError {
		t.Fatalf("unsupported plan accepted: %+v %v", unsupported, err)
	}
}

func TestMCPHostClientReadsProjectLocalFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("INFISICAL_CLIENT_ID=id\nINFISICAL_CLIENT_SECRET=sensitive-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := hostClient("https://infisical.example", func(key string) string {
		if key == "KUCHDESK_PROJECT_ROOT" {
			return root
		}
		return ""
	})
	if err != nil || !client.Configured() {
		t.Fatalf("MCP host client did not load private file: %v", err)
	}
}

func TestMCPDeployProfileBoundary(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	chart := filepath.Join(source, "chart")
	profiles := filepath.Join(root, "profiles")
	for _, dir := range []string{chart, profiles} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(source, "Dockerfile"), filepath.Join(chart, "Chart.yaml")} {
		if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	spec := deploy.Spec{SourceDir: source, Dockerfile: filepath.Join(source, "Dockerfile"), ChartDir: chart, ImageRepository: "localhost:5001/kuchdesk-agent", ImageTag: "git-123456789abc", KubeContext: "docker-desktop", Namespace: "development-tools", Release: "kuchdesk-agent", Deployment: "kuchdesk-agent", PreflightDomain: "infisical.local.dev", PreflightRegistryURL: "http://127.0.0.1:5001"}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "agent.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.json"), filepath.Join(profiles, "escape.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runner := async.NewRunner(ctx, 2, 4)
	defer runner.Close()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithDeploy(infisical.NewService(fakeNameLister{}), runner, deployConfig{ProfileDir: profiles, Preflight: mcpHealthyPreflight{}}).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-harness"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	plan, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_deploy_plan", Arguments: map[string]any{"profile": "agent.json"}})
	if err != nil || plan.IsError {
		t.Fatalf("plan failed: %v %+v", err, plan)
	}
	encoded, _ := json.Marshal(plan.StructuredContent)
	if !strings.Contains(string(encoded), "kuchdesk-agent") {
		t.Fatalf("unexpected plan: %s", encoded)
	}
	preflight, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_deploy_preflight", Arguments: map[string]any{"profile": "agent.json"}})
	if err != nil || preflight.IsError {
		t.Fatalf("preflight failed: %v %+v", err, preflight)
	}
	encoded, _ = json.Marshal(preflight.StructuredContent)
	if !strings.Contains(string(encoded), `"ready":true`) {
		t.Fatalf("unexpected preflight result: %s", encoded)
	}
	for _, profile := range []string{"../outside.json", "escape.json"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_deploy_plan", Arguments: map[string]any{"profile": profile}})
		if err != nil || !result.IsError {
			t.Fatalf("unsafe profile accepted: %s %v %+v", profile, err, result)
		}
	}
	start, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_deploy_start", Arguments: map[string]any{"profile": "agent.json", "confirm": "kuchdesk-agent/development-tools"}})
	if err != nil || !start.IsError {
		t.Fatalf("disabled deploy accepted: %v %+v", err, start)
	}
	pushClient, pushServer := mcp.NewInMemoryTransports()
	pushSession, err := newServerWithDeploy(infisical.NewService(fakeNameLister{}), runner, deployConfig{ProfileDir: profiles, Enabled: true, Runner: deployCommandRunner{}, Preflight: mcpHealthyPreflight{}}).Connect(ctx, pushServer, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pushSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "deploy-test"}, nil).Connect(ctx, pushClient, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	bad, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_deploy_start", Arguments: map[string]any{"profile": "agent.json", "confirm": "wrong/namespace"}})
	if err != nil || !bad.IsError {
		t.Fatalf("missing exact confirmation accepted: %v %+v", err, bad)
	}
	planSteps := []map[string]any{
		{"tool": "kuchdesk_deploy_plan", "profile": "agent.json"},
		{"tool": "kuchdesk_deploy_preflight", "profile": "agent.json"},
		{"tool": "kuchdesk_deploy_start", "profile": "agent.json", "confirm": "kuchdesk-agent/development-tools"},
		{"tool": "kuchdesk_doctor_verify", "checkIds": []string{"service:development-tools/kuchdesk-agent"}},
	}
	validated, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_doctor_validate_plan", Arguments: map[string]any{"steps": planSteps}})
	if err != nil || validated.IsError {
		t.Fatalf("valid typed plan rejected: %+v %v", validated, err)
	}
	encoded, _ = json.Marshal(validated.StructuredContent)
	if !strings.Contains(string(encoded), `"valid":true`) {
		t.Fatalf("plan validation missing: %s", encoded)
	}
	started, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_deploy_start", Arguments: map[string]any{"profile": "agent.json", "confirm": "kuchdesk-agent/development-tools"}})
	if err != nil || started.IsError {
		t.Fatalf("confirmed deployment failed: %v %+v", err, started)
	}
	encoded, _ = json.Marshal(started.StructuredContent)
	var job jobStarted
	if err := json.Unmarshal(encoded, &job); err != nil || job.ID == "" {
		t.Fatalf("missing deployment job ID: %s %v", encoded, err)
	}
	deadline := time.After(time.Second)
	for {
		status, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_job_status", Arguments: map[string]any{"id": job.ID}})
		if err != nil || status.IsError {
			t.Fatalf("deployment status failed: %v %+v", err, status)
		}
		encoded, _ = json.Marshal(status.StructuredContent)
		var output jobOutput
		if err := json.Unmarshal(encoded, &output); err != nil {
			t.Fatal(err)
		}
		if output.State == async.Succeeded {
			if output.Deployment == nil || output.Deployment.Digest == "" {
				t.Fatalf("deployment result missing digest: %+v", output)
			}
			break
		}
		if output.State == async.Failed {
			t.Fatalf("deployment failed: %+v", output)
		}
		select {
		case <-deadline:
			t.Fatalf("deployment timed out: %+v", output)
		case <-time.After(time.Millisecond):
		}
	}
}

type fakeNameLister struct{}

type deployCommandRunner struct{}

func (deployCommandRunner) Run(context.Context, string, string, ...string) error { return nil }
func (deployCommandRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	if name == "git" && args[0] == "rev-parse" {
		return []byte("123456789abc\n"), nil
	}
	if name == "git" && args[0] == "status" {
		return nil, nil
	}
	if name == "docker" && args[0] == "image" {
		if args[1] == "ls" {
			return nil, nil
		}
		if len(args) == 3 {
			return nil, os.ErrNotExist
		}
	}
	if name == "docker" && args[0] == "image" {
		return []byte(`["localhost:5001/kuchdesk-agent@sha256:` + strings.Repeat("a", 64) + `"]`), nil
	}
	return nil, os.ErrInvalid
}

func (fakeNameLister) Configured() bool { return true }
func (fakeNameLister) ListSecretNames(_ context.Context, _, _, _ string) ([]infisical.SecretName, error) {
	return []infisical.SecretName{{Name: "DB_USER", Path: "/app"}}, nil
}

func TestMCPAsyncNameListCompletes(t *testing.T) {
	ctx := context.Background()
	runner := async.NewRunner(ctx, 2, 4)
	defer runner.Close()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithRunner(infisical.NewService(fakeNameLister{}), runner).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-harness"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	started, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "infisical_start_list_secret_names", Arguments: map[string]any{
		"projectId": "project", "environment": "dev", "secretPath": "/app",
	}})
	if err != nil || started.IsError {
		t.Fatalf("start failed: %v %+v", err, started)
	}
	var start jobStarted
	encoded, _ := json.Marshal(started.StructuredContent)
	if err := json.Unmarshal(encoded, &start); err != nil || start.ID == "" {
		t.Fatalf("invalid job ID: %v %s", err, encoded)
	}
	deadline := time.After(time.Second)
	for {
		status, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "infisical_job_status", Arguments: map[string]any{"id": start.ID}})
		if err != nil || status.IsError {
			t.Fatalf("status failed: %v %+v", err, status)
		}
		encoded, _ := json.Marshal(status.StructuredContent)
		var output jobOutput
		if err := json.Unmarshal(encoded, &output); err != nil {
			t.Fatal(err)
		}
		if output.State == async.Succeeded {
			if len(output.Secrets) != 1 || output.Secrets[0].Name != "DB_USER" {
				t.Fatalf("unexpected name-only result: %+v", output)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("job did not finish: %+v", output)
		case <-time.After(time.Millisecond):
		}
	}
}
