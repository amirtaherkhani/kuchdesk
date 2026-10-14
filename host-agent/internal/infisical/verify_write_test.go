package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type fixturePolicy struct{ deny string }

func (p fixturePolicy) Authorize(_ context.Context, c Command) (Command, map[string]bool, error) {
	if c.Operation == p.deny {
		return c, nil, ErrPolicyDenied
	}
	return c, map[string]bool{"fixture-project": true}, nil
}

func TestVerifySecretWriteMatchMismatchAndCleanup(t *testing.T) {
	for _, mode := range []string{"success", "mismatch", "schema", "permission", "cleanup-failure", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			value, name := "", ""
			exists := false
			creates, deletes := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/universal-auth/login" {
					commandLogin(w)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if !strings.HasPrefix(r.URL.Path, "/api/v4/secrets/KUCHDESK_VERIFY_") {
					t.Error("unexpected fixture endpoint")
					w.WriteHeader(400)
					return
				}
				if r.Method == "POST" || r.Method == "PATCH" {
					var body struct {
						Value string `json:"secretValue"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Error("invalid body")
					}
					value = body.Value
					name = strings.TrimPrefix(r.URL.Path, "/api/v4/secrets/")
					exists = true
					if r.Method == "POST" {
						creates++
					}
					if mode == "canceled" {
						cancel()
					}
					w.Write([]byte(`{}`))
					return
				}
				if r.Method == "DELETE" {
					deletes++
					if mode == "cleanup-failure" {
						w.WriteHeader(403)
						return
					}
					exists = false
					w.Write([]byte(`{}`))
					return
				}
				if !exists {
					w.WriteHeader(404)
					return
				}
				observed := value
				if r.URL.Query().Get("viewSecretValue") == "true" {
					if mode == "permission" {
						w.WriteHeader(403)
						return
					}
					if mode == "schema" {
						w.Write([]byte(`{"secret":{}}`))
						return
					}
					if mode == "mismatch" {
						observed = "wrong fixture marker"
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"secret": map[string]any{"secretKey": name, "secretValue": observed}})
			})
			service := CommandService{Client: client, MCPPolicy: fixturePolicy{}}
			result, err := service.VerifySecretWrite(ctx, VerifyWriteInput{ProjectID: "fixture-project", Environment: "dev", Path: "/fixture", Confirm: "secret.verify_write:fixture-project/dev/fixture"})
			mu.Lock()
			defer mu.Unlock()
			if creates != 1 || deletes != 1 {
				t.Fatalf("fixture requests create=%d delete=%d", creates, deletes)
			}
			if mode == "success" && (err != nil || !result.CreatedMatch || !result.UpdatedMatch || !result.MismatchRejected || !result.CleanupConfirmed) {
				t.Fatal("successful verification failed")
			}
			if mode == "cleanup-failure" {
				if result.CleanupConfirmed || result.CleanupError != "permission_denied" || !exists {
					t.Fatal("cleanup failure hidden")
				}
			} else if !result.CleanupConfirmed || exists {
				t.Fatal("fixture cleanup not confirmed")
			}
			if mode != "success" && mode != "cleanup-failure" && err == nil {
				t.Fatal("negative verification accepted")
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), value) || strings.Contains(string(encoded), "wrong fixture marker") {
				t.Fatal("value disclosed")
			}
		})
	}
}

func TestVerifySecretWriteRejectsBeforeUpstream(t *testing.T) {
	for _, mode := range []string{"policy", "cleanup-policy", "readonly", "static", "confirmation", "scope"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := commandClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
			service := CommandService{Client: client, AllowedProjects: map[string]bool{"fixture-project": true}}
			input := VerifyWriteInput{ProjectID: "fixture-project", Environment: "dev", Path: "/fixture", Confirm: "secret.verify_write:fixture-project/dev/fixture"}
			switch mode {
			case "policy":
				service.MCPPolicy = fixturePolicy{deny: "secret.create"}
			case "cleanup-policy":
				service.MCPPolicy = fixturePolicy{deny: "secret.delete"}
			case "readonly":
				service.ReadOnly = true
			case "static":
				service.AllowedProjects = nil
			case "confirmation":
				input.Confirm = "wrong"
			case "scope":
				input.ProjectID = ""
			}
			_, err := service.VerifySecretWrite(context.Background(), input)
			if err == nil || calls != 0 {
				t.Fatal("denial had upstream effects")
			}
			if mode == "policy" || mode == "cleanup-policy" {
				if !errors.Is(err, ErrPolicyDenied) {
					t.Fatal("lost policy error")
				}
			}
		})
	}
}
