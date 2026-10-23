package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
)

type fakeCommands struct{}

func (fakeCommands) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	switch name {
	case "colima":
		if strings.Contains(strings.Join(args, " "), "nerdctl") {
			return []byte(`{"Driver":"overlayfs","ServerVersion":"v2.1.3"}`), nil
		}
		if strings.Contains(strings.Join(args, " "), "machine-id") {
			return []byte("fixture-machine"), nil
		}
		return []byte("running"), nil
	case "route":
		return []byte("interface: en0\n"), nil
	case "kubectl":
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "get nodes") {
			return []byte(`{"items":[{"status":{"nodeInfo":{"kubeletVersion":"v1.35.0+k3s1","containerRuntimeVersion":"containerd://29.0.0","machineID":"fixture-machine"},"conditions":[{"type":"Ready","status":"True"}]}}]}`), nil
		}
		if strings.Contains(joined, "top nodes") {
			return []byte("node 100m 10% 1024Mi 20%\n"), nil
		}
		return []byte(`{"spec":{"replicas":1},"status":{"readyReplicas":1}}`), nil
	}
	return nil, errors.New("unexpected command")
}

type fakeResolver struct{}

func (fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}, nil
}

func TestCLIReportAndMCPOnlyAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	service := doctor.Service{Commands: fakeCommands{}, Resolver: fakeResolver{}, HTTP: server.Client(), DiskFree: func(string) (uint64, error) { return 5 << 30, nil }}
	args := []string{"-domain", "grafana.local.dev", "-kube-context", "colima-kuchdesk-fresh", "-registry-url", server.URL, "-disk-path", t.TempDir(), "-service", "development-tools/kuchdesk-agent"}
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut, service); code != 0 || !strings.Contains(out.String(), `"healthy":true`) || !strings.Contains(out.String(), `"kuchdesk.doctor/v2"`) {
		t.Fatalf("doctor CLI failed: code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	bad := append(append([]string(nil), args...), "-service", "../other")
	if code := run(bad, &out, &errOut, service); code != 2 || out.Len() != 0 {
		t.Fatalf("unsafe CLI profile accepted: code=%d out=%s", code, out.String())
	}
	out.Reset()
	if code := run(append(args, "-ai"), &out, &errOut, service); code != 2 {
		t.Fatal("CLI unexpectedly accepted a separate AI mode")
	}
}
