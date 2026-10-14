package infisical

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroupWritePlansChecksVersionsAndAppliesWithoutValuesInResult(t *testing.T) {
	writes := 0
	client := commandClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/universal-auth/login" {
			commandLogin(w)
			return
		}
		if r.Method == http.MethodGet {
			if r.URL.Path == "/api/v4/secrets/NEW" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if r.URL.Path == "/api/v4/secrets/OLD" {
				_, _ = io.WriteString(w, `{"secret":{"secretKey":"OLD","secretPath":"/app","version":3,"secretValue":"do-not-return"}}`)
				return
			}
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v4/secrets/NEW" {
			writes++
			_, _ = io.WriteString(w, `{}`)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	s := CommandService{Client: client, AllowedProjects: map[string]bool{"project-1": true}, ProvisionDir: filepath.Join(t.TempDir(), "provisions")}
	input := GroupWriteInput{ProjectID: "project-1", Environment: "dev", Path: "/app", Changes: []GroupChange{{Name: "OLD", Action: "update"}, {Name: "NEW", Action: "create"}}, PlanOnly: true}
	plan, err := s.GroupWrite(context.Background(), input)
	if err != nil || len(plan.Items) != 2 || plan.Items[0].Version != 3 || plan.Items[1].Exists || writes != 0 {
		t.Fatal("group plan failed")
	}
	newValue := "synthetic"
	input.Changes = []GroupChange{{Name: "NEW", Action: "create", Value: &newValue}}
	input.PlanOnly = false
	input.Confirm = "secret.batch:project-1/dev/app"
	result, err := s.GroupWrite(context.Background(), input)
	if err != nil || writes != 1 || result.Items[0].Status != "applied" {
		t.Fatal("group apply failed")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), newValue) {
		t.Fatal("group result exposed a value")
	}
	input.Changes = []GroupChange{{Name: "OLD", Action: "update", Value: &newValue, ExpectedVersion: ptrInt(2)}}
	result, err = s.GroupWrite(context.Background(), input)
	if err != ErrVersionConflict || writes != 1 || result.Items[0].Status != "version_conflict" {
		t.Fatal("version conflict failed")
	}
}

func ptrInt(v int) *int { return &v }
