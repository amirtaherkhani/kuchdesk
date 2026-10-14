package infisical

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
)

type provisionAuthorizer struct{}

func (provisionAuthorizer) Authorize(_ context.Context, c Command) (Command, map[string]bool, error) {
	return c, map[string]bool{}, nil
}

func TestProjectProvisionOmittedSlugAndRetry(t *testing.T) {
	creates := 0
	client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			commandLogin(w)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects":
			creates++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["slug"] != nil || body["projectName"] != "dev-fixture" {
				t.Error("native project create schema changed")
			}
			_, _ = io.WriteString(w, `{"project":{"id":"new-project","name":"dev-fixture","slug":"dev-fixture-generated"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/new-project":
			_, _ = io.WriteString(w, `{"project":{"id":"new-project","name":"dev-fixture","slug":"dev-fixture-generated","type":"secret-manager","environments":[{"id":"env-1","name":"Development","slug":"dev"}]}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects":
			_, _ = io.WriteString(w, `{"projects":[{"id":"new-project"}]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	s := CommandService{Client: client, MCPPolicy: provisionAuthorizer{}, AllowProjectCreate: true, ProvisionDir: filepath.Join(t.TempDir(), "provisions")}
	cmd := Command{Operation: "project.create", Name: "dev-fixture", RequestID: "run-1", Confirm: "project.create:run-1"}
	for i := 0; i < 2; i++ {
		result, err := s.Execute(context.Background(), cmd)
		if err != nil || len(result.Items) != 1 || result.Items[0].Slug != "dev-fixture-generated" || len(result.Items[0].Environments) != 1 || result.EnrollmentStatus != "host_identity_visible" {
			t.Fatal("native provision or readback failed")
		}
	}
	if creates != 1 {
		t.Fatal("retry created a duplicate project")
	}
}
