package hostbridge

import (
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef"

func TestClientRequiresHTTPSAndValidBearer(t *testing.T) {
	for _, raw := range []string{"http://host.lima.internal:8181", "https://user:password@host:8181", "https://host:8181/commands", "https://host:8181?key=value"} {
		if _, err := New(raw, testToken, ""); err == nil {
			t.Fatalf("accepted invalid URL %q", raw)
		}
	}
	if _, err := New("https://host:8181", "short", ""); err == nil {
		t.Fatal("accepted short bridge bearer")
	}
}

func TestClientReachesHostDoctorOverTrustedTLS(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/doctor" || r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("unexpected host request path or authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schemaVersion":"kuchdesk.doctor/v2","healthy":true}`)
	}))
	defer upstream.Close()
	certificate := upstream.Certificate()
	if certificate == nil {
		t.Fatal("test TLS server has no certificate")
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})
	caFile := filepath.Join(t.TempDir(), "host-ca.pem")
	if err := os.WriteFile(caFile, ca, 0o600); err != nil {
		t.Fatal(err)
	}
	bridge, err := New(upstream.URL, testToken, caFile)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/host/doctor", strings.NewReader(`{"domain":"grafana.local.dev"}`))
	response := httptest.NewRecorder()
	bridge.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "kuchdesk.doctor/v2") || calls.Load() != 1 {
		t.Fatalf("Doctor proxy = %d %s, calls=%d", response.Code, response.Body, calls.Load())
	}

}

func TestClientRejectsUntrustedCertificateAndOversizedResponse(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, strings.Repeat("x", 1<<20+1))
	}))
	defer upstream.Close()
	bridge, err := New(upstream.URL, testToken, "")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	bridge.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/host/doctor", strings.NewReader(`{}`)))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("untrusted TLS response = %d", response.Code)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw})
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, ca, 0o600); err != nil {
		t.Fatal(err)
	}
	bridge, err = New(upstream.URL, testToken, path)
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	bridge.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/host/doctor", strings.NewReader(`{}`)))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("oversized response = %d", response.Code)
	}
}

func TestClusterProxyHasNoSecretCommandRoute(t *testing.T) {
	c := &Client{}
	response := httptest.NewRecorder()
	c.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/host/infisical/commands", strings.NewReader(`{}`)))
	if response.Code != http.StatusNotFound {
		t.Fatalf("removed route returned %d", response.Code)
	}
}
