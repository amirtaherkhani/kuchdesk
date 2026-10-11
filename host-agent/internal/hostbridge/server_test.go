package hostbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/infisical"
)

const bridgeToken = "0123456789abcdef0123456789abcdef"

type fakeCommands struct {
	call func(context.Context, infisical.Command) (infisical.CommandResult, error)
}

func (f fakeCommands) Execute(ctx context.Context, input infisical.Command) (infisical.CommandResult, error) {
	return f.call(ctx, input)
}

type fakeDoctor struct{}

func (fakeDoctor) Run(_ context.Context, _ doctor.Config) (doctor.Report, error) {
	return doctor.Report{SchemaVersion: "kuchdesk.doctor/v2", Healthy: true}, nil
}

func authorizedRequest(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+bridgeToken)
	return r
}

func TestHostBridgeAuthorizesAndBoundsCommands(t *testing.T) {
	server, err := New(fakeCommands{call: func(_ context.Context, c infisical.Command) (infisical.CommandResult, error) {
		return infisical.CommandResult{Operation: c.Operation, Target: c.ProjectID}, nil
	}}, fakeDoctor{}, bridgeToken)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/infisical/commands", strings.NewReader(`{"operation":"project.list"}`)),
		func() *http.Request {
			r := authorizedRequest("/v1/infisical/commands", `{"operation":"project.list"}`)
			r.Header.Set("Authorization", "Bearer wrong")
			return r
		}(),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized request returned %d", response.Code)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest("/v1/infisical/commands", `{"operation":"project.list","unknown":1}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field returned %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest("/v1/infisical/commands", `{"operation":"project.list"}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"operation":"project.list"`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authorized response = %d %s", response.Code, response.Body)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest("/v1/doctor", `{"domain":"grafana.local.dev","kubeContext":"docker-desktop","registryUrl":"http://127.0.0.1:5001","diskPath":"/tmp"}`))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"schemaVersion":"kuchdesk.doctor/v2"`) {
		t.Fatalf("Doctor response = %d %s", response.Code, response.Body)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedRequest("/v1/doctor", `{"domain":"invalid","kubeContext":"","registryUrl":"http://127.0.0.1:5001","diskPath":"relative"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid Doctor config returned %d", response.Code)
	}
}

func TestHostBridgeMapsErrorsWithoutUpstreamBodies(t *testing.T) {
	server, _ := New(fakeCommands{call: func(context.Context, infisical.Command) (infisical.CommandResult, error) {
		return infisical.CommandResult{}, &infisical.APIError{StatusCode: http.StatusForbidden}
	}}, fakeDoctor{}, bridgeToken)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authorizedRequest("/v1/infisical/commands", `{"operation":"project.list"}`))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "upstream_permission_denied") {
		t.Fatalf("mapped error = %d %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), "secretValue") {
		t.Fatal("error response exposed upstream data")
	}
}

func TestHostBridgeLimitsConcurrentRequests(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	server, _ := New(fakeCommands{call: func(_ context.Context, c infisical.Command) (infisical.CommandResult, error) {
		entered <- struct{}{}
		<-release
		return infisical.CommandResult{Operation: c.Operation}, nil
	}}, fakeDoctor{}, bridgeToken)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, authorizedRequest("/v1/infisical/commands", `{"operation":"project.list"}`))
			if response.Code != http.StatusOK {
				t.Errorf("queued response = %d", response.Code)
			}
		}()
	}
	for range 8 {
		<-entered
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, authorizedRequest("/v1/infisical/commands", `{"operation":"project.list"}`))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("ninth request = %d", response.Code)
	}
	close(release)
	group.Wait()
}

func TestCommandResponseHasNoValueField(t *testing.T) {
	server, _ := New(fakeCommands{call: func(_ context.Context, c infisical.Command) (infisical.CommandResult, error) {
		return infisical.CommandResult{Operation: c.Operation, Applied: true}, nil
	}}, fakeDoctor{}, bridgeToken)
	value := "sensitive-input"
	input, _ := json.Marshal(infisical.Command{Operation: "secret.create", ProjectID: "project-1", Value: &value})
	r := authorizedRequest("/v1/infisical/commands", string(input))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, r)
	output, _ := io.ReadAll(bytes.NewReader(response.Body.Bytes()))
	if strings.Contains(string(output), value) || strings.Contains(string(output), bridgeToken) {
		t.Fatalf("secret echoed in response")
	}
}

func TestBridgeRejectsEveryAdministrativeOperationBeforeCommander(t *testing.T) {
	server, _ := New(fakeCommands{call: func(context.Context, infisical.Command) (infisical.CommandResult, error) {
		t.Fatal("K8 invoked host admin commander")
		return infisical.CommandResult{}, nil
	}}, fakeDoctor{}, bridgeToken)
	for _, op := range []string{"project.rename", "project.create", "project.delete", "secret.create", "secret.update", "secret.delete", "membership.add", "membership.list", "role.create", "grafana.admin.password", "grafana.admin.password.reset", "future.operation"} {
		body, _ := json.Marshal(map[string]string{"operation": op, "projectId": "any-project"})
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, authorizedRequest("/v1/infisical/commands", string(body)))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s returned %d", op, w.Code)
		}
	}
}
