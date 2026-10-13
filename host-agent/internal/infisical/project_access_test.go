package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProjectAccessMatrix(t *testing.T) {
	tests := []struct {
		name, listing, detail string
		detailStatus          int
		want                  ProjectAccess
	}{
		{"org-admin-without-project-membership", `{"projects":[]}`, `{"message":"not a member","secret":"never-return"}`, 403, ProjectAccess{Slug: "team-example", DetailStatus: "forbidden"}},
		{"explicit-project-membership", `{"projects":[{"slug":"team-example"}]}`, `{"id":"project-1","slug":"team-example","secret":"never-return"}`, 200, ProjectAccess{Slug: "team-example", Listed: true, DetailStatus: "allowed", ProjectID: "project-1"}},
		{"unknown-project", `{"projects":[]}`, `{"message":"not found"}`, 404, ProjectAccess{Slug: "team-example", DetailStatus: "not_found"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch r.URL.Path {
				case "/api/v1/auth/universal-auth/login":
					fmt.Fprint(w, `{"accessToken":"short-lived","expiresIn":60,"tokenType":"Bearer"}`)
				case "/api/v1/projects":
					if r.Header.Get("Authorization") != "Bearer short-lived" {
						t.Error("missing bearer on project list")
					}
					fmt.Fprint(w, tt.listing)
				case "/api/v1/projects/slug/team-example":
					w.WriteHeader(tt.detailStatus)
					fmt.Fprint(w, tt.detail)
				default:
					t.Errorf("unexpected API path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "id", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.InspectProjectAccess(context.Background(), "team-example")
			if err != nil || got != tt.want || requests.Load() != 3 {
				t.Fatalf("access=%+v err=%v calls=%d", got, err, requests.Load())
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "never-return") {
				t.Fatal("response body leaked from access report")
			}
		})
	}
}

func TestListProjectIDsReturnsStableIDsOnly(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			fmt.Fprint(w, `{"accessToken":"token","expiresIn":60,"tokenType":"Bearer"}`)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/projects" {
			t.Errorf("unexpected project request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"projects":[{"id":"project-1","slug":"one","name":"One"},{"id":"project-2","slug":"two","secretValue":"never-return"}]}`)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "id", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := client.ListProjectIDs(context.Background())
	if err != nil || !ids["project-1"] || !ids["project-2"] || len(ids) != 2 {
		t.Fatalf("project ids = %#v, err=%v", ids, err)
	}
	encoded, _ := json.Marshal(ids)
	if strings.Contains(string(encoded), "never-return") || strings.Contains(string(encoded), "One") {
		t.Fatalf("project metadata leaked through ID listing: %s", encoded)
	}
}

func TestProjectAccessRejectsUnsafeAndInvalidResponses(t *testing.T) {
	tests := []struct {
		name, listing, detail string
		listingStatus         int
	}{
		{"list-forbidden", `{"secret":"never-return"}`, ``, 403},
		{"missing-projects-field", `{}`, ``, 200},
		{"null-project-list", `{"projects":null}`, ``, 200},
		{"malformed-detail", `{"projects":[]}`, `{"project":{"id":"project-1","slug":"team-example"}}`, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/universal-auth/login":
					fmt.Fprint(w, `{"accessToken":"token","expiresIn":60,"tokenType":"Bearer"}`)
				case "/api/v1/projects":
					if tt.listingStatus != 0 {
						w.WriteHeader(tt.listingStatus)
					}
					fmt.Fprint(w, tt.listing)
				case "/api/v1/projects/slug/team-example":
					fmt.Fprint(w, tt.detail)
				default:
					t.Error("unsafe or unexpected request")
				}
			}))
			defer server.Close()
			client, _ := NewClient(server.URL, "id", "secret", server.Client())
			_, err := client.InspectProjectAccess(context.Background(), "team-example")
			if err == nil || strings.Contains(err.Error(), "never-return") {
				t.Fatalf("invalid response accepted or leaked: %v", err)
			}
		})
	}
	client, _ := NewClient("https://example.invalid", "", "", nil)
	for _, slug := range []string{"", "../team-example", "team/example", "Team-Example", strings.Repeat("a", 129)} {
		if _, err := client.InspectProjectAccess(context.Background(), slug); err == nil {
			t.Errorf("accepted invalid slug %q", slug)
		}
	}
	if _, err := client.InspectProjectAccess(context.Background(), "team-example"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("missing auth should fail before project read: %v", err)
	}
}

func TestSecretListRejectsMalformedOrOversizedScopeResponse(t *testing.T) {
	oversized := `{"secrets":[` + strings.TrimSuffix(strings.Repeat(`{"secretKey":"KEY"},`, 1001), ",") + `]}`
	for name, body := range map[string]string{
		"missing-list":   `{}`,
		"null-list":      `{"secrets":null}`,
		"missing-name":   `{"secrets":[{"secretPath":"/app"}]}`,
		"oversized-list": oversized,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/universal-auth/login" {
					fmt.Fprint(w, `{"accessToken":"token","expiresIn":60,"tokenType":"Bearer"}`)
					return
				}
				if r.URL.Path != "/api/v4/secrets" || r.URL.Query().Get("projectId") != "project-1" || r.URL.Query().Get("environment") != "dev" || r.URL.Query().Get("secretPath") != "/app" {
					t.Error("unexpected secret scope")
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			client, _ := NewClient(server.URL, "id", "secret", server.Client())
			if names, err := client.ListSecretNames(context.Background(), "project-1", "dev", "/app"); err == nil || len(names) != 0 {
				t.Fatalf("malformed list accepted: %+v %v", names, err)
			}
		})
	}
}
