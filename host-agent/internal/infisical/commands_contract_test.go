package infisical

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestAllManagementOperationsUseScopedEndpoints(t *testing.T) {
	value := "fixture-value"
	base := Command{ProjectID: "fixture-project", Environment: "dev", Path: "/fixture", ID: "fixture-id", IdentityID: "fixture-identity", Name: "fixture-name", Slug: "fixture-slug", Value: &value, Roles: []string{"viewer"}, Permissions: []PermissionRule{{Subject: "secrets", Action: "read"}}}
	tests := []struct {
		operation, method, path, response string
	}{
		{"project.list", "GET", "/api/v1/projects", `{"projects":[]}`},
		{"project.get", "GET", "/api/v1/projects/fixture-project", `{"project":{"id":"fixture-project"}}`},
		{"project.create", "POST", "/api/v1/projects", `{"project":{"id":"created"}}`},
		{"project.rename", "PATCH", "/api/v1/projects/fixture-project", `{}`},
		{"project.update", "PATCH", "/api/v1/projects/fixture-project", `{}`},
		{"project.delete", "DELETE", "/api/v1/projects/fixture-project", `{}`},
		{"environment.list", "GET", "/api/v1/projects/fixture-project", `{"project":{"environments":[]}}`},
		{"environment.create", "POST", "/api/v1/projects/fixture-project/environments", `{}`},
		{"environment.update", "PATCH", "/api/v1/projects/fixture-project/environments/fixture-id", `{}`},
		{"environment.delete", "DELETE", "/api/v1/projects/fixture-project/environments/fixture-id", `{}`},
		{"folder.list", "GET", "/api/v1/folders", `{"folders":[]}`},
		{"folder.create", "POST", "/api/v1/folders", `{}`},
		{"folder.update", "PATCH", "/api/v1/folders/fixture-id", `{}`},
		{"folder.delete", "DELETE", "/api/v1/folders/fixture-id", `{}`},
		{"secret.list", "GET", "/api/v4/secrets", `{"secrets":[]}`},
		{"secret.get", "GET", "/api/v4/secrets/fixture-name", `{"secret":{"id":"fixture-id","secretKey":"fixture-name","secretValue":"never-return"}}`},
		{"secret.create", "POST", "/api/v4/secrets/fixture-name", `{}`},
		{"secret.update", "PATCH", "/api/v4/secrets/fixture-name", `{}`},
		{"secret.delete", "DELETE", "/api/v4/secrets/fixture-name", `{}`},
		{"role.list", "GET", "/api/v1/projects/fixture-project/roles", `{"roles":[]}`},
		{"role.create", "POST", "/api/v1/projects/fixture-project/roles", `{}`},
		{"role.update", "PATCH", "/api/v1/projects/fixture-project/roles/fixture-id", `{}`},
		{"role.delete", "DELETE", "/api/v1/projects/fixture-project/roles/fixture-id", `{}`},
		{"membership.list", "GET", "/api/v1/projects/fixture-project/identity-memberships", `{"identityMemberships":[]}`},
		{"membership.add", "POST", "/api/v1/projects/fixture-project/identity-memberships/fixture-identity", `{}`},
		{"membership.update", "PATCH", "/api/v1/projects/fixture-project/identity-memberships/fixture-identity", `{}`},
		{"membership.delete", "DELETE", "/api/v1/projects/fixture-project/identity-memberships/fixture-identity", `{}`},
	}
	if len(tests) != len(SupportedOperations()) {
		t.Fatal("operation registry and contract matrix differ")
	}
	for _, tc := range tests {
		t.Run(tc.operation, func(t *testing.T) {
			calls := 0
			client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/universal-auth/login" {
					commandLogin(w)
					return
				}
				calls++
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, tc.method, tc.path)
				}
				if tc.operation == "secret.get" && r.URL.Query().Get("viewSecretValue") != "false" {
					t.Error("ordinary secret read requested value")
				}
				_, _ = io.WriteString(w, tc.response)
			})
			command := base
			command.Operation = tc.operation
			target, write, err := validateCommand(command)
			if err != nil {
				t.Fatal(err)
			}
			if write {
				command.Confirm = tc.operation + ":" + target
			}
			service := CommandService{Client: client, AllowedProjects: map[string]bool{base.ProjectID: true}, AllowProjectCreate: true}
			result, err := service.Execute(context.Background(), command)
			if err != nil || calls != 1 || result.Operation != tc.operation || result.Applied != write {
				t.Fatalf("operation failed: result=%+v err=%v calls=%d", result, err, calls)
			}
		})
	}
}
