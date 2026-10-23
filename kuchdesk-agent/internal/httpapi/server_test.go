package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amirtaherkhani/kuchdesk/kuchdesk-agent/internal/dnsconfig"
	"github.com/amirtaherkhani/kuchdesk/kuchdesk-agent/internal/model"
	"github.com/amirtaherkhani/kuchdesk/kuchdesk-agent/internal/stream"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

type staticSource struct {
	ready    bool
	snapshot model.Snapshot
}

func (s staticSource) Ready() bool {
	return s.ready
}

func (s staticSource) Snapshot() model.Snapshot {
	return s.snapshot
}

func TestSnapshotRequiresBearerToken(t *testing.T) {
	t.Parallel()

	source := staticSource{
		ready: true,
		snapshot: model.Snapshot{
			SchemaVersion: model.SchemaVersion,
			Cluster:       model.Cluster{ID: "homelab"},
			Services: []model.Service{{
				Name:         "internal-service",
				ExternalIPs:  []string{},
				ExternalURLs: []string{},
			}},
		},
	}
	server := New(
		source,
		stream.NewBroker("homelab", 8),
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}),
		"test-token",
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	request := httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, body = %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), model.SchemaVersion) {
		t.Fatalf("snapshot response does not contain schema version: %s", response.Body)
	}
	for _, expected := range []string{`"externalIPs":[]`, `"externalURLs":[]`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("snapshot response does not preserve empty service array %s: %s", expected, response.Body)
		}
	}
}

func TestHostBridgeRoutesRequireAgentBearerAndStayAbsentByDefault(t *testing.T) {
	server := New(
		staticSource{ready: true},
		stream.NewBroker("homelab", 8),
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}),
		"agent-token",
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	path := "/v1/host/doctor"
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled host bridge returned %d", response.Code)
	}
	server.SetHostBridgeHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			t.Errorf("wrong host bridge path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized host route returned %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer agent-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authorized host route returned %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/manage/capabilities", nil)
	request.Header.Set("Authorization", "Bearer agent-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("host bridge enabled Kubernetes management: %d", response.Code)
	}
}

func TestEventsRejectsRequestsUntilInformerCachesAreReady(t *testing.T) {
	t.Parallel()

	server := New(
		staticSource{ready: false},
		stream.NewBroker("homelab", 8),
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}),
		"test-token",
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	request := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), "still syncing") {
		t.Fatalf("syncing event response = %d %s", response.Code, response.Body)
	}
}

func TestSSEStartsWithSnapshotAndSupportsStreaming(t *testing.T) {
	t.Parallel()

	source := staticSource{
		ready: true,
		snapshot: model.Snapshot{
			SchemaVersion: model.SchemaVersion,
			Cluster: model.Cluster{
				ID:   "homelab",
				Name: "Home lab",
			},
		},
	}
	broker := stream.NewBroker("homelab", 8)
	server := httptest.NewServer(New(
		source,
		broker,
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}),
		"test-token",
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Content-Type = %q", contentType)
	}

	scanner := bufio.NewScanner(response.Body)
	lines := make([]string, 0, 8)
	for scanner.Scan() && len(lines) < 6 {
		lines = append(lines, scanner.Text())
	}
	joined := strings.Join(lines, "\n")
	for _, expected := range []string{
		"retry: 3000",
		"event: snapshot",
		`"clusterId":"homelab"`,
		model.SchemaVersion,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("SSE output missing %q:\n%s", expected, joined)
		}
	}
	if _, err := broker.Publish("snapshot", model.Snapshot{
		SchemaVersion: model.SchemaVersion,
		Cluster:       model.Cluster{ID: "homelab", Name: "Updated live cluster"},
	}); err != nil {
		t.Fatal(err)
	}
	liveBlock := readSSEBlock(t, scanner)
	if !strings.Contains(liveBlock, "id: 1") ||
		!strings.Contains(liveBlock, "Updated live cluster") {
		t.Fatalf("live SSE block = %q", liveBlock)
	}
}

func TestSSEHistoryGapSendsCurrentSnapshotWithoutStaleReplay(t *testing.T) {
	t.Parallel()

	broker := stream.NewBroker("homelab", 2)
	for generation := 1; generation <= 4; generation++ {
		if _, err := broker.Publish("snapshot", map[string]int{"generation": generation}); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(New(
		staticSource{
			ready: true,
			snapshot: model.Snapshot{
				SchemaVersion: model.SchemaVersion,
				Cluster:       model.Cluster{ID: "homelab", Name: "Current cluster"},
			},
		},
		broker,
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}),
		"test-token",
		20*time.Millisecond,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", "1")
	request.Header.Set("Authorization", "Bearer test-token")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	scanner := bufio.NewScanner(response.Body)
	retryBlock := readSSEBlock(t, scanner)
	currentBlock := readSSEBlock(t, scanner)
	nextBlock := readSSEBlock(t, scanner)
	if retryBlock != "retry: 3000" {
		t.Fatalf("retry block = %q", retryBlock)
	}
	if !strings.Contains(currentBlock, "id: 4") ||
		!strings.Contains(currentBlock, "Current cluster") {
		t.Fatalf("current snapshot block = %q", currentBlock)
	}
	if !strings.HasPrefix(nextBlock, ": heartbeat") {
		t.Fatalf("history gap replayed stale data: %q", nextBlock)
	}
}

func TestSSEInitialConnectionSkipsHistoricalReplay(t *testing.T) {
	t.Parallel()

	broker := stream.NewBroker("homelab", 8)
	if _, err := broker.Publish("snapshot", map[string]string{"name": "Historical cluster"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(
		staticSource{
			ready: true,
			snapshot: model.Snapshot{
				SchemaVersion: model.SchemaVersion,
				Cluster:       model.Cluster{ID: "homelab", Name: "Current cluster"},
			},
		},
		broker,
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}),
		"test-token",
		20*time.Millisecond,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer test-token")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	scanner := bufio.NewScanner(response.Body)
	retryBlock := readSSEBlock(t, scanner)
	currentBlock := readSSEBlock(t, scanner)
	nextBlock := readSSEBlock(t, scanner)
	if retryBlock != "retry: 3000" {
		t.Fatalf("retry block = %q", retryBlock)
	}
	if !strings.Contains(currentBlock, "id: 1") ||
		!strings.Contains(currentBlock, "Current cluster") {
		t.Fatalf("current snapshot block = %q", currentBlock)
	}
	if !strings.HasPrefix(nextBlock, ": heartbeat") {
		t.Fatalf("initial connection replayed historical data: %q", nextBlock)
	}
}

func TestDNSConfigurationEndpointAppliesAliasesAndPublishesEvent(t *testing.T) {
	t.Parallel()

	kube := kubernetesfake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "grafana", Namespace: "monitoring"}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "coredns",
				Namespace:       "kube-system",
				ResourceVersion: "8",
			},
			Data: map[string]string{"Corefile": ".:53 {\n    kubernetes cluster.local\n    reload\n}\n"},
		},
	)
	broker := stream.NewBroker("homelab", 8)
	_, live, _, _, unsubscribe := broker.Subscribe(0)
	defer unsubscribe()
	server := New(
		staticSource{ready: true},
		broker,
		dnsconfig.New(kube, dnsconfig.Options{
			Enabled:       true,
			Namespace:     "kube-system",
			ConfigMapName: "coredns",
			CorefileKey:   "Corefile",
			ClusterDomain: "cluster.local",
		}),
		"test-token",
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	request := httptest.NewRequest(
		http.MethodPut,
		"/v1/dns/config",
		strings.NewReader(`{
			"resourceVersion":"8",
			"aliases":[{
				"hostname":"grafana.home.arpa",
				"service":"grafana",
				"namespace":"monitoring"
			}]
		}`),
	)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", response.Code, response.Body)
	}
	var state dnsconfig.State
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Aliases) != 1 || state.Aliases[0].Hostname != "grafana.home.arpa" {
		t.Fatalf("unexpected DNS state: %#v", state)
	}
	select {
	case event := <-live:
		if event.Name != "dns.config.changed" {
			t.Fatalf("DNS event name = %q", event.Name)
		}
	case <-time.After(time.Second):
		t.Fatal("DNS event was not published")
	}
}

func TestDNSConfigurationEndpointIsReadOnlyWhenDisabled(t *testing.T) {
	t.Parallel()

	server := New(
		staticSource{ready: true},
		stream.NewBroker("homelab", 8),
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{
			Namespace:     "kube-system",
			ConfigMapName: "coredns",
			CorefileKey:   "Corefile",
			ClusterDomain: "cluster.local",
		}),
		"test-token",
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	getRequest := httptest.NewRequest(http.MethodGet, "/v1/dns/config", nil)
	getRequest.Header.Set("Authorization", "Bearer test-token")
	getResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"enabled":false`) {
		t.Fatalf("GET status = %d, body = %s", getResponse.Code, getResponse.Body)
	}

	putRequest := httptest.NewRequest(
		http.MethodPut,
		"/v1/dns/config",
		strings.NewReader(`{"resourceVersion":"1","aliases":[]}`),
	)
	putRequest.Header.Set("Authorization", "Bearer test-token")
	putResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusForbidden {
		t.Fatalf("PUT status = %d, body = %s", putResponse.Code, putResponse.Body)
	}
}

func TestDNSConfigurationEndpointRejectsAmbiguousJSON(t *testing.T) {
	t.Parallel()

	server := New(
		staticSource{ready: true},
		stream.NewBroker("homelab", 8),
		dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}),
		"test-token",
		time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	for name, body := range map[string]string{
		"unknown field": `{"resourceVersion":"1","aliases":[],"replaceCorefile":true}`,
		"two objects":   `{"resourceVersion":"1","aliases":[]} {"aliases":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, "/v1/dns/config", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s", response.Code, response.Body)
			}
		})
	}
}

func readSSEBlock(t *testing.T, scanner *bufio.Scanner) string {
	t.Helper()
	lines := make([]string, 0, 4)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(lines) > 0 {
				return strings.Join(lines, "\n")
			}
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatal("SSE stream ended before the next block")
	return ""
}

func TestManagementHandlerRemainsDisabledUntilConfiguredAndUsesBearerAuth(t *testing.T) {
	server := New(staticSource{ready: true}, stream.NewBroker("test", 8), dnsconfig.New(kubernetesfake.NewSimpleClientset(), dnsconfig.Options{}), "test-token", time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	path := "/v1/manage/capabilities"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled status = %d", response.Code)
	}
	server.SetManagementHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d", response.Code)
	}
}

func TestMissingBearerFailsClosedAndHostFileRoutesAreAbsent(t *testing.T) {
	server := New(staticSource{ready: true}, stream.NewBroker("test", 8), nil, "", time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, path := range []string{"/v1/platform", "/v1/snapshot", "/v1/events", "/v1/dns/config"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer ")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s status = %d", path, response.Code)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(method, "/v1/config/values/agent/values.local.yaml", nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("direct host file route status = %d", response.Code)
		}
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("probe %s status = %d", path, response.Code)
		}
	}
}
