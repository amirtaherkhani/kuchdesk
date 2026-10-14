package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func commandClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "identity", "bootstrap-secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func commandLogin(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"accessToken":"short-lived","expiresIn":3600,"tokenType":"Bearer"}`)
}

func TestCommandServiceSecretReadIsRedactedAndProjectScoped(t *testing.T) {
	var calls atomic.Int32
	client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			commandLogin(w)
			return
		}
		calls.Add(1)
		if r.URL.Path != "/api/v4/secrets/API_KEY" || r.URL.Query().Get("viewSecretValue") != "false" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"secret":{"id":"secret-1","secretKey":"API_KEY","secretPath":"/app","secretValue":"must-not-leak"}}`)
	})
	service := CommandService{Client: client, AllowedProjects: map[string]bool{"project-1": true}}
	command := Command{Operation: "secret.get", ProjectID: "project-1", Environment: "dev", Path: "/app", Name: "API_KEY"}
	result, err := service.Execute(context.Background(), command)
	if err != nil || len(result.Items) != 1 || result.Items[0].Name != "API_KEY" {
		t.Fatalf("secret metadata: %+v, %v", result, err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "bootstrap-secret") {
		t.Fatalf("secret value escaped redaction: %s", encoded)
	}
	command.ProjectID = "other-project"
	if _, err := service.Execute(context.Background(), command); !errors.Is(err, ErrProjectDenied) {
		t.Fatalf("unlisted project error = %v", err)
	}
	command.ProjectID = "project-1"
	command.Path = "/../other"
	if _, err := service.Execute(context.Background(), command); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("invalid path error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected upstream calls = %d", calls.Load())
	}
}

func TestProjectListReturnsOnlyAllowedProjects(t *testing.T) {
	client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			commandLogin(w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/projects" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		_, _ = io.WriteString(w, `{"projects":[{"id":"allowed","name":"Allowed"},{"id":"private","name":"Private"}]}`)
	})
	service := CommandService{Client: client, AllowedProjects: map[string]bool{"allowed": true}}
	result, err := service.Execute(context.Background(), Command{Operation: "project.list"})
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != "allowed" {
		t.Fatalf("filtered project list = %+v, %v", result, err)
	}
}

func TestCommandWriteNeedsExactConfirmationAndNeverReplays401(t *testing.T) {
	var writes atomic.Int32
	var logins atomic.Int32
	client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			logins.Add(1)
			commandLogin(w)
			return
		}
		writes.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v4/secrets/API_KEY" {
			t.Errorf("unexpected write: %s %s", r.Method, r.URL)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"secretValue":"input-value"`) {
			t.Errorf("write did not send requested value")
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"secretValue":"upstream-value"}`)
	})
	service := CommandService{Client: client, AllowedProjects: map[string]bool{"project-1": true}}
	value := "input-value"
	command := Command{Operation: "secret.create", ProjectID: "project-1", Environment: "dev", Path: "/app", Name: "API_KEY", Value: &value}
	if _, err := service.Execute(context.Background(), command); !errors.Is(err, ErrConfirmation) || writes.Load() != 0 {
		t.Fatalf("missing confirmation reached upstream: %v", err)
	}
	target, _, _ := validateCommand(command)
	command.Confirm = command.Operation + ":" + target
	_, err := service.Execute(context.Background(), command)
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != http.StatusUnauthorized || writes.Load() != 1 || logins.Load() != 1 {
		t.Fatalf("write replay or error leak: err=%v writes=%d logins=%d", err, writes.Load(), logins.Load())
	}
	if strings.Contains(err.Error(), "upstream-value") {
		t.Fatal("upstream secret leaked in error")
	}
}

func TestAdministrativeCommandsUseVersionPinnedEndpoints(t *testing.T) {
	tests := []struct {
		command Command
		method  string
		path    string
	}{
		{Command{Operation: "project.create", Name: "Test project", Slug: "test-project"}, http.MethodPost, "/api/v1/projects"},
		{Command{Operation: "environment.create", ProjectID: "project-1", Name: "Stage", Slug: "stage"}, http.MethodPost, "/api/v1/projects/project-1/environments"},
		{Command{Operation: "folder.create", ProjectID: "project-1", Environment: "dev", Path: "/", Name: "app"}, http.MethodPost, "/api/v1/folders"},
		{Command{Operation: "role.create", ProjectID: "project-1", Name: "Operator", Slug: "operator", Permissions: []PermissionRule{{Subject: "secrets", Action: "read"}}}, http.MethodPost, "/api/v1/projects/project-1/roles"},
		{Command{Operation: "membership.add", ProjectID: "project-1", IdentityID: "identity-1", Roles: []string{"admin"}}, http.MethodPost, "/api/v1/projects/project-1/identity-memberships/identity-1"},
	}
	for _, tc := range tests {
		t.Run(tc.command.Operation, func(t *testing.T) {
			client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/universal-auth/login" {
					commandLogin(w)
					return
				}
				if tc.command.Operation == "project.create" && r.Method == http.MethodGet {
					if r.URL.Path == "/api/v1/projects/new-project" {
						_, _ = io.WriteString(w, `{"project":{"id":"new-project","name":"Test project","slug":"test-project","environments":[]}}`)
						return
					}
					if r.URL.Path == "/api/v1/projects" {
						_, _ = io.WriteString(w, `{"projects":[{"id":"new-project"}]}`)
						return
					}
				}
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("got %s %s, want %s %s", r.Method, r.URL.Path, tc.method, tc.path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"project":{"id":"new-project","name":"Test project","slug":"test-project","secretValue":"ignored"}}`)
			})
			service := CommandService{Client: client, AllowedProjects: map[string]bool{"project-1": true}, AllowProjectCreate: true}
			target, _, err := validateCommand(tc.command)
			if err != nil {
				t.Fatal(err)
			}
			command := tc.command
			command.Confirm = command.Operation + ":" + target
			result, err := service.Execute(context.Background(), command)
			if err != nil || !result.Applied {
				t.Fatalf("result = %+v, %v", result, err)
			}
			if command.Operation == "project.create" && (len(result.Items) != 1 || result.Items[0].ID != "new-project") {
				t.Fatalf("project creation did not return its ID: %+v", result)
			}
		})
	}
}

func TestProjectRenamePreservesUnspecifiedMetadata(t *testing.T) {
	calls := 0
	client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			commandLogin(w)
			return
		}
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != "PATCH" || len(body) != 1 || body["name"] != "Platform Example" {
			t.Error("rename changed fields other than name")
		}
		_, _ = io.WriteString(w, `{"project":{"id":"stable-id","name":"Platform Example","slug":"existing-slug"}}`)
	})
	s := CommandService{Client: client, AllowedProjects: map[string]bool{"stable-id": true}}
	c := Command{Operation: "project.rename", ProjectID: "stable-id", Name: "Platform Example", Confirm: "project.rename:stable-id"}
	if _, e := s.Execute(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("rename repeated")
	}
}
