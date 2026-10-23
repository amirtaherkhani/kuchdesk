package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/async"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeDoctorRunner struct{}

func (fakeDoctorRunner) Run(context.Context, doctor.Config) (doctor.Report, error) {
	return doctor.Report{SchemaVersion: "kuchdesk.doctor/v2", Healthy: true}, nil
}

func TestMCPDirectYAMLReadRedactsAndEditWritesCanonicalFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab/apps/platform/technitium/values.local.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("adminPassword: synthetic-one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runner := async.NewRunner(ctx, 1, 2)
	defer runner.Close()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServerWithScope(runner, deployConfig{ProjectRoot: root}, fakeDoctorRunner{}, "doctor-repair").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "direct-yaml-test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	read, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_chart_values_read", Arguments: map[string]any{"service": "technitium", "file": "values.local.yaml"}})
	if err != nil || read.IsError {
		t.Fatal("direct YAML read failed")
	}
	encoded, err := json.Marshal(read.StructuredContent)
	if err != nil || strings.Contains(string(encoded), "synthetic-one") {
		t.Fatal("private YAML read exposed a value")
	}
	write, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_chart_values_set", Arguments: map[string]any{"service": "technitium", "file": "values.local.yaml", "keys": []string{"adminPassword"}, "valueYaml": "synthetic-two"}})
	if err != nil || write.IsError {
		t.Fatal("direct YAML edit failed")
	}
	encoded, err = json.Marshal(write.StructuredContent)
	if err != nil || strings.Contains(string(encoded), "synthetic-two") {
		t.Fatal("direct YAML edit returned a value")
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "synthetic-two") {
		t.Fatal("canonical private YAML was not edited")
	}
}
func TestMCPRegistersOnlyDoctorAndDeploymentTools(t *testing.T) {
	for _, scope := range []string{"doctor-readonly", "doctor-repair"} {
		t.Run(scope, func(t *testing.T) {
			root := t.TempDir()
			script := filepath.Join(root, "lab", "scripts", "service_access.py")
			if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(script, []byte("print('{\"context\":\"colima-kuchdesk-fresh\",\"services\":{}}')\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			runner := async.NewRunner(ctx, 2, 4)
			defer runner.Close()
			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			serverSession, err := newServerWithScope(runner, deployConfig{ProjectRoot: root}, fakeDoctorRunner{}, scope).Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer serverSession.Close()
			client, err := mcp.NewClient(&mcp.Implementation{Name: "registration-test"}, nil).Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			catalog, err := client.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			known := map[string]bool{}
			for _, tool := range catalog.Tools {
				if len(tool.Name) >= 10 && tool.Name[:10] == "infisical_" {
					t.Fatalf("removed tool registered: %s", tool.Name)
				}
				known[tool.Name] = true
			}
			if !known["kuchdesk_capabilities"] || !known["kuchdesk_doctor"] || !known["kuchdesk_doctor_verify"] {
				t.Fatal("required Doctor tools missing")
			}
			if !known["kuchdesk_service_access_status"] {
				t.Fatal("read-only service access status tool missing")
			}
			result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "kuchdesk_service_access_status", Arguments: map[string]any{}})
			if err != nil || result.IsError {
				t.Fatalf("read-only service access status call failed: err=%v result=%+v", err, result)
			}
			data, err := json.Marshal(result.StructuredContent)
			if err != nil || !strings.Contains(string(data), "colima-kuchdesk-fresh") {
				t.Fatal("service access report missing from MCP result")
			}
			if !known["kuchdesk_chart_values_catalog"] || !known["kuchdesk_chart_values_read"] || known["kuchdesk_chart_values_set"] != (scope == "doctor-repair") {
				t.Fatal("direct YAML tool scope mismatch")
			}
			if known["kuchdesk_deploy_start"] != (scope == "doctor-repair") {
				t.Fatal("deployment scope mismatch")
			}
		})
	}
}
