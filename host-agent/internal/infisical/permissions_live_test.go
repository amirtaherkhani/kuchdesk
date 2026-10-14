package infisical

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"testing"
)

// This opt-in test uses only a disposable project and a fresh no-access
// identity. It never prints bootstrap credentials or synthetic secret values.
func TestLiveDisposablePermissionTransitions(t *testing.T) {
	if os.Getenv("KUCHDESK_LIVE_PERMISSION_TEST") != "1" {
		t.Skip("live disposable permission test not enabled")
	}
	projectID := os.Getenv("KUCHDESK_LIVE_PERMISSION_PROJECT_ID")
	if !resourceSegment.MatchString(projectID) {
		t.Fatal("disposable project ID required")
	}
	id, secret, err := HostCredentials(os.Getenv)
	if err != nil {
		t.Fatal(PublicError(err))
	}
	client, err := NewClient(os.Getenv("KUCHDESK_LIVE_SECRET_URL"), id, secret, nil)
	if err != nil {
		t.Fatal("invalid live origin")
	}
	ctx := context.Background()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal("fixture entropy unavailable")
	}
	marker := hex.EncodeToString(random)
	name := "codex-" + marker
	created, err := client.request(ctx, http.MethodPost, "/api/v1/identities", nil, map[string]any{"name": name, "organizationId": os.Getenv("KUCHDESK_LIVE_ORGANIZATION_ID"), "role": "no-access", "hasDeleteProtection": false})
	if err != nil {
		t.Fatal(PublicError(err))
	}
	var identity struct {
		Identity struct {
			ID string `json:"id"`
		} `json:"identity"`
	}
	if json.Unmarshal(created, &identity) != nil || !resourceSegment.MatchString(identity.Identity.ID) {
		t.Fatal("identity response invalid")
	}
	defer func() {
		if _, err := client.request(ctx, http.MethodDelete, "/api/v1/identities/"+identity.Identity.ID, nil, nil); err != nil {
			t.Errorf("identity cleanup: %s", PublicError(err))
		}
	}()
	auth, err := client.request(ctx, http.MethodPost, "/api/v1/auth/universal-auth/identities/"+identity.Identity.ID, nil, map[string]any{})
	if err != nil {
		t.Fatal(PublicError(err))
	}
	var authResult struct {
		Auth struct {
			ClientID string `json:"clientId"`
		} `json:"identityUniversalAuth"`
	}
	if json.Unmarshal(auth, &authResult) != nil || authResult.Auth.ClientID == "" {
		t.Fatal("universal auth response invalid")
	}
	issued, err := client.request(ctx, http.MethodPost, "/api/v1/auth/universal-auth/identities/"+identity.Identity.ID+"/client-secrets", nil, map[string]any{"description": "codex disposable permission test", "numUsesLimit": 20})
	if err != nil {
		t.Fatal(PublicError(err))
	}
	var issuedResult struct {
		ClientSecret string `json:"clientSecret"`
	}
	if json.Unmarshal(issued, &issuedResult) != nil || issuedResult.ClientSecret == "" {
		t.Fatal("client secret response invalid")
	}
	fixtureClient, err := NewClient(os.Getenv("KUCHDESK_LIVE_SECRET_URL"), authResult.Auth.ClientID, issuedResult.ClientSecret, nil)
	if err != nil {
		t.Fatal("fixture client unavailable")
	}
	admin := CommandService{Client: client, AllowedProjects: map[string]bool{projectID: true}}
	membership := func(op, role string) error {
		cmd := Command{Operation: op, ProjectID: projectID, IdentityID: identity.Identity.ID, Confirm: op + ":" + projectID + "/" + identity.Identity.ID}
		if role != "" {
			cmd.Roles = []string{role}
		}
		_, err := admin.Execute(ctx, cmd)
		return err
	}
	if err := membership("membership.add", "admin"); err != nil {
		t.Fatal(PublicError(err))
	}
	revoked := false
	defer func() {
		if revoked {
			return
		}
		if err := membership("membership.delete", ""); err != nil {
			var api *APIError
			if !errors.As(err, &api) || api.StatusCode != http.StatusNotFound {
				t.Errorf("membership cleanup: %s", PublicError(err))
			}
		}
	}()
	key := "CODEX_PERMISSION_" + marker
	value := "synthetic-" + marker
	secretPath := "/api/v4/secrets/" + key
	fields := map[string]any{"projectId": projectID, "environment": "dev", "secretPath": "/", "secretValue": value}
	if _, err := fixtureClient.request(ctx, http.MethodPost, secretPath, nil, fields); err != nil {
		t.Fatalf("admin write: %s", PublicError(err))
	}
	defer func() {
		if _, err := client.request(ctx, http.MethodDelete, secretPath, nil, map[string]any{"projectId": projectID, "environment": "dev", "secretPath": "/"}); err != nil {
			t.Errorf("secret cleanup: %s", PublicError(err))
		}
	}()
	query := url.Values{"projectId": {projectID}, "environment": {"dev"}, "secretPath": {"/"}, "viewSecretValue": {"true"}}
	if _, err := fixtureClient.get(ctx, secretPath, query); err != nil {
		t.Fatalf("admin read: %s", PublicError(err))
	}
	if err := membership("membership.update", "viewer"); err != nil {
		t.Fatal(PublicError(err))
	}
	if _, err := fixtureClient.get(ctx, secretPath, query); err != nil {
		t.Fatalf("viewer read: %s", PublicError(err))
	}
	if _, err := fixtureClient.request(ctx, http.MethodPatch, secretPath, nil, fields); !isForbidden(err) {
		t.Fatal("viewer write was not denied")
	}
	if err := membership("membership.delete", ""); err != nil {
		t.Fatal(PublicError(err))
	}
	revoked = true
	if _, err := fixtureClient.get(ctx, secretPath, query); err == nil {
		t.Fatal("revoked identity retained read access")
	}
}

func isForbidden(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.StatusCode == http.StatusForbidden
}
