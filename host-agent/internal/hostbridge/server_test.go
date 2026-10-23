package hostbridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
)

const bridgeToken = "0123456789abcdef0123456789abcdef"

type fakeDoctor struct{}

func (fakeDoctor) Run(_ context.Context, _ doctor.Config) (doctor.Report, error) {
	return doctor.Report{SchemaVersion: "kuchdesk.doctor/v2", Healthy: true}, nil
}
func authorizedRequest(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+bridgeToken)
	return r
}
func TestHostBridgeOnlyExposesAuthenticatedDoctor(t *testing.T) {
	server, err := New(fakeDoctor{}, bridgeToken)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	for _, tc := range []struct {
		path string
		body string
		want int
	}{
		{"/v1/doctor", `{}`, http.StatusUnauthorized},
		{"/v1/infisical/commands", `{}`, http.StatusNotFound},
		{"/v1/doctor", `{"domain":"grafana.local.dev","kubeContext":"colima-kuchdesk-fresh","registryUrl":"http://127.0.0.1:5001","diskPath":"/tmp"}`, http.StatusOK},
		{"/v1/doctor", `{"unknown":1}`, http.StatusBadRequest},
	} {
		request := authorizedRequest(tc.path, tc.body)
		if tc.want == http.StatusUnauthorized {
			request.Header.Del("Authorization")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.path, response.Code, tc.want)
		}
	}
}

func TestServiceAccessRequiresBearerAndReturnsReport(t *testing.T) {
	server, err := New(fakeDoctor{}, bridgeToken)
	if err != nil {
		t.Fatal(err)
	}
	server.ServiceAccess = func(context.Context) (map[string]any, error) {
		return map[string]any{"profile": "kuchdesk-fresh"}, nil
	}
	for _, authorized := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/v1/service-access", nil)
		if authorized {
			req.Header.Set("Authorization", "Bearer "+bridgeToken)
		}
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		want := http.StatusUnauthorized
		if authorized {
			want = http.StatusOK
		}
		if res.Code != want {
			t.Fatalf("authorized=%v: status=%d, want=%d", authorized, res.Code, want)
		}
		if authorized && !strings.Contains(res.Body.String(), "kuchdesk-fresh") {
			t.Fatal("missing service access report")
		}
	}
}
