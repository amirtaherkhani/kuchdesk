package infisical

import (
	"context"
	"io"
	"net/http"
	"testing"
)

func TestReadValuesExactScopeAndNoCrossScopeResults(t *testing.T) {
	client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			commandLogin(w)
			return
		}
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/api/v4/secrets" || q.Get("projectId") != "project-1" || q.Get("environment") != "dev" || q.Get("secretPath") != "/app" || q.Get("recursive") != "false" || q.Get("include_imports") != "false" || q.Get("viewSecretValue") != "true" {
			t.Error("value read widened the requested scope")
		}
		_, _ = io.WriteString(w, `{"secrets":[{"secretKey":"A","secretValue":"fixture-a","secretPath":"/app"},{"secretKey":"B","secretValue":"fixture-b","secretPath":"/app"}]}`)
	})
	s := CommandService{Client: client, AllowedProjects: map[string]bool{"project-1": true}}
	result, err := s.ReadValues(context.Background(), ValueInput{ProjectID: "project-1", Environment: "dev", Path: "/app"})
	if err != nil || len(result.Items) != 2 || result.Items[0].Value != "fixture-a" {
		t.Fatal("scoped value read failed")
	}
	page, err := s.ReadValues(context.Background(), ValueInput{ProjectID: "project-1", Environment: "dev", Path: "/app", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "A" || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatal("first value page failed")
	}
	page, err = s.ReadValues(context.Background(), ValueInput{ProjectID: "project-1", Environment: "dev", Path: "/app", Limit: 1, Offset: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "B" || page.NextOffset != nil {
		t.Fatal("second value page failed")
	}
	if _, err := s.ReadValues(context.Background(), ValueInput{ProjectID: "project-2", Environment: "dev", Path: "/app"}); err != ErrProjectDenied {
		t.Fatal("project boundary failed")
	}
	s.ReadOnly = true
	if _, err := s.ReadValues(context.Background(), ValueInput{ProjectID: "project-1", Environment: "dev", Path: "/app"}); err != ErrProjectDenied {
		t.Fatal("Kubernetes boundary failed")
	}
}
