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

// This opt-in fixture reads a value only for the random secret it created.
// Neither the expected nor observed value enters an MCP result or test log.
func TestLiveSecretWriteVerification(t *testing.T) {
	if os.Getenv("KUCHDESK_LIVE_SECRET_TEST") != "1" {
		t.Skip("live secret fixture not enabled")
	}
	projectID := os.Getenv("KUCHDESK_LIVE_SECRET_PROJECT_ID")
	environment := os.Getenv("KUCHDESK_LIVE_SECRET_ENVIRONMENT")
	path := os.Getenv("KUCHDESK_LIVE_SECRET_PATH")
	if !resourceSegment.MatchString(projectID) || !resourceSegment.MatchString(environment) || !validCommandPath(path) {
		t.Fatal("live fixture needs one valid, explicit project ID, environment, and path")
	}
	clientID, clientSecret, err := HostCredentials(os.Getenv)
	if err != nil {
		t.Fatal(PublicError(err))
	}
	client, err := NewClient(os.Getenv("KUCHDESK_LIVE_SECRET_URL"), clientID, clientSecret, nil)
	if err != nil {
		t.Fatal("invalid live fixture URL")
	}
	ctx := context.Background()
	service := CommandService{Client: client, AllowedProjects: map[string]bool{projectID: true}}
	marker := func() string {
		bytes := make([]byte, 24)
		if _, err := rand.Read(bytes); err != nil {
			t.Fatal("cannot generate fixture marker")
		}
		return hex.EncodeToString(bytes)
	}
	name := "CODEX_MCP_FIXTURE_" + marker()
	base := Command{ProjectID: projectID, Environment: environment, Path: path, Name: name}
	readValue := func() (string, error) {
		body, err := client.get(ctx, "/api/v4/secrets/"+name, url.Values{
			"projectId": {projectID}, "environment": {environment}, "secretPath": {path}, "viewSecretValue": {"true"},
		})
		if err != nil {
			return "", err
		}
		var result struct {
			Secret struct {
				Value *string `json:"secretValue"`
			} `json:"secret"`
		}
		if json.Unmarshal(body, &result) != nil || result.Secret.Value == nil {
			return "", ErrInvalidResponse
		}
		return *result.Secret.Value, nil
	}
	if _, err := readValue(); err == nil {
		t.Fatal("random fixture name already exists")
	} else {
		var api *APIError
		if !errors.As(err, &api) || api.StatusCode != http.StatusNotFound {
			t.Fatal(PublicError(err))
		}
	}
	apply := func(operation string, value *string) error {
		command := base
		command.Operation, command.Value = operation, value
		target, _, err := validateCommand(command)
		if err != nil {
			return err
		}
		command.Confirm = operation + ":" + target
		_, err = service.Execute(ctx, command)
		return err
	}
	deleted := false
	// A failed create can have reached upstream. The random name is owned by
	// this run, so cleanup is safe even when the response was lost.
	defer func() {
		if deleted {
			return
		}
		if err := apply("secret.delete", nil); err != nil {
			t.Errorf("fixture cleanup failed for %s: %s", name, PublicError(err))
		}
	}()
	first := marker()
	if err := apply("secret.create", &first); err != nil {
		t.Fatal(PublicError(err))
	}
	if observed, err := readValue(); err != nil || observed != first {
		if err != nil {
			t.Fatalf("created fixture could not be read: %s", PublicError(err))
		}
		t.Fatal("created fixture value did not match")
	}
	second := marker()
	if err := apply("secret.update", &second); err != nil {
		t.Fatal(PublicError(err))
	}
	if observed, err := readValue(); err != nil || observed != second || observed == first {
		if err != nil {
			t.Fatalf("updated fixture could not be read: %s", PublicError(err))
		}
		t.Fatal("updated fixture value did not match")
	}
	if err := apply("secret.delete", nil); err != nil {
		t.Fatal(PublicError(err))
	}
	deleted = true
	if _, err := readValue(); err == nil {
		t.Fatal("deleted fixture is still readable")
	} else {
		var api *APIError
		if !errors.As(err, &api) || api.StatusCode != http.StatusNotFound {
			t.Fatal(PublicError(err))
		}
	}
	t.Logf("verified and removed test-owned secret %s in %s/%s%s", name, projectID, environment, path)
}
