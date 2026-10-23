package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type commandFake struct {
	outputs map[string][]byte
	errors  map[string]error
}

func (f commandFake) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	return f.outputs[key], f.errors[key]
}

type resolverFake struct {
	addresses []net.IPAddr
	err       error
}

func (f resolverFake) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return f.addresses, f.err
}

func fixture(t *testing.T) (Service, Config) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	t.Cleanup(server.Close)
	commands := commandFake{errors: map[string]error{}, outputs: map[string][]byte{

		"colima --profile kuchdesk-fresh ssh -- cat /etc/machine-id":                                         []byte("fixture-machine"),
		"colima --profile kuchdesk-fresh status":                                                             []byte("running"),
		"route -n get 192.0.2.1":                                                                             []byte("interface: en0\npassword=LEAKED_SECRET\n"),
		"colima --profile kuchdesk-fresh nerdctl -- --namespace k8s.io info --format {{json .}}":             []byte(`{"Driver":"overlayfs","ServerVersion":"v2.1.3"}`),
		"kubectl --context colima-kuchdesk-fresh get nodes -o json":                                          []byte(`{"items":[{"status":{"nodeInfo":{"kubeletVersion":"v1.35.0+k3s1","containerRuntimeVersion":"containerd://29.0.0","machineID":"fixture-machine"},"conditions":[{"type":"Ready","status":"True"}]},"token":"LEAKED_SECRET"}]}`),
		"kubectl --context colima-kuchdesk-fresh top nodes --no-headers":                                     []byte("colima-kuchdesk-fresh 367m 3% 6928Mi 24%\n"),
		"kubectl --context colima-kuchdesk-fresh -n development-tools get deployment kuchdesk-agent -o json": []byte(`{"spec":{"replicas":1},"status":{"readyReplicas":1},"metadata":{"annotations":{"token":"LEAKED_SECRET"}}}`),
	}}
	service := Service{Commands: commands, Resolver: resolverFake{addresses: []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}}, HTTP: server.Client(), DiskFree: func(string) (uint64, error) { return 5 << 30, nil }}
	config := Config{Domain: "grafana.local.dev", KubeContext: "colima-kuchdesk-fresh", RegistryURL: server.URL, DiskPath: t.TempDir(), Services: []Target{{Namespace: "development-tools", Deployment: "kuchdesk-agent"}}}
	return service, config
}

func TestDoctorSanitizesAndStructuresChecks(t *testing.T) {
	service, config := fixture(t)
	report, err := service.Run(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Healthy || len(report.Findings) != 0 || report.SchemaVersion != "kuchdesk.doctor/v2" || report.Summary.Total != 9 || report.Summary.Passed != 9 || report.GeneratedAt.IsZero() {
		t.Fatalf("unexpected report: %+v", report)
	}
	want := []string{"network", "dns", "colima", "containerd", "kubernetes", "disk", "resources", "registry", "service:development-tools/kuchdesk-agent"}
	for i, id := range want {
		if report.Checks[i].ID != id || report.Checks[i].Status != "ok" || report.Checks[i].Component == "" || report.Checks[i].Source == "" || report.Checks[i].Context == "" || report.Checks[i].ObservedAt.IsZero() {
			t.Fatalf("check %d: %+v", i, report.Checks[i])
		}
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "LEAKED_SECRET") || strings.Contains(string(encoded), "192.0.2.1") || !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatal("raw diagnostics or address escaped sanitization")
	}
}

func TestDoctorRejectsContextNamesPointingAtAnotherStoreOrVM(t *testing.T) {
	for _, command := range []string{"colima --profile kuchdesk-fresh nerdctl -- --namespace k8s.io info --format {{json .}}", "colima --profile kuchdesk-fresh ssh -- cat /etc/machine-id"} {
		service, cfg := fixture(t)
		fake := service.Commands.(commandFake)
		fake.outputs[command] = []byte("different-identity")
		service.Commands = fake
		report, err := service.Run(context.Background(), cfg)
		if err != nil || report.Healthy {
			t.Fatal("context name alone was accepted as runtime identity")
		}
	}
}

func TestDoctorReportsFailuresWithoutRawErrors(t *testing.T) {
	service, config := fixture(t)
	fake := service.Commands.(commandFake)
	fake.errors["colima --profile kuchdesk-fresh nerdctl -- --namespace k8s.io info --format {{json .}}"] = errors.New("token=LEAKED_SECRET")
	fake.outputs["kubectl --context colima-kuchdesk-fresh get nodes -o json"] = []byte(`{"items":[{"status":{"nodeInfo":{"kubeletVersion":"v1.35.0+k3s1","containerRuntimeVersion":"containerd://29.0.0","machineID":"fixture-machine"},"conditions":[]}}]}`)
	service.Commands = fake
	service.Resolver = resolverFake{err: errors.New("LEAKED_SECRET")}
	service.DiskFree = func(string) (uint64, error) { return 1 << 30, nil }
	report, err := service.Run(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if report.Healthy || len(report.Findings) < 3 {
		t.Fatalf("failures omitted: %+v", report)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "LEAKED_SECRET") {
		t.Fatal("raw error leaked")
	}
	for _, finding := range report.Findings {
		if finding.Recommendation == "" {
			t.Fatalf("finding lacks action: %+v", finding)
		}
	}
}

func TestDoctorRejectsUnsafeInput(t *testing.T) {
	_, config := fixture(t)
	for _, change := range []func(*Config){
		func(c *Config) { c.Domain = "bad_domain" },
		func(c *Config) { c.KubeContext = ";rm -rf /" },
		func(c *Config) { c.RegistryURL = "https://example.com" },
		func(c *Config) { c.RegistryURL = "http://user:pass@127.0.0.1:5001" },
		func(c *Config) { c.DiskPath = "relative" },
		func(c *Config) { c.Services = append(c.Services, c.Services[0]) },
	} {
		candidate := config
		change(&candidate)
		if err := candidate.Validate(); err == nil {
			t.Fatalf("unsafe configuration accepted: %+v", candidate)
		}
	}
}

func TestDoctorConcurrentRequestsRemainIndependent(t *testing.T) {
	service, config := fixture(t)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			report, err := service.Run(context.Background(), config)
			if err != nil || !report.Healthy {
				t.Errorf("concurrent report: %+v %v", report, err)
			}
		}()
	}
	group.Wait()
}

func TestDoctorChecksConfiguredIngressRouteInsteadOfVPNDefaultRoute(t *testing.T) {
	service, config := fixture(t)
	fake := service.Commands.(commandFake)
	fake.outputs["route -n get 192.0.2.1"] = []byte("interface: bridge100\n")
	service.Commands = fake
	report, err := service.Run(context.Background(), config)
	if err != nil || !report.Healthy || report.Checks[0].Status != "ok" || len(report.Findings) != 0 {
		t.Fatalf("local ingress route report: %+v %v", report, err)
	}
	fake.outputs["route -n get 192.0.2.1"] = []byte("interface: utun6\n")
	service.Commands = fake
	report, err = service.Run(context.Background(), config)
	if err != nil || !report.Healthy || report.Checks[0].Status != "warn" || report.Checks[0].ErrorClass != "vpn_route" {
		t.Fatalf("VPN ingress route report: %+v %v", report, err)
	}
}

func TestDoctorReportsNodePressure(t *testing.T) {
	service, config := fixture(t)
	fake := service.Commands.(commandFake)
	fake.outputs["kubectl --context colima-kuchdesk-fresh top nodes --no-headers"] = []byte("node 950m 95% 1024Mi 91%\n")
	service.Commands = fake
	report, err := service.Run(context.Background(), config)
	if err != nil || report.Healthy || report.Checks[6].Status != "fail" || report.Checks[6].Evidence["maxCPUPercent"] != 95 {
		t.Fatalf("pressure report: %+v %v", report.Checks[6], err)
	}
	fake.outputs["kubectl --context colima-kuchdesk-fresh top nodes --no-headers"] = []byte("node 850m 85% 1024Mi 84%\n")
	report, err = service.Run(context.Background(), config)
	if err != nil || !report.Healthy || report.Checks[6].Status != "warn" {
		t.Fatalf("warning report: %+v %v", report.Checks[6], err)
	}
}

func TestRegistryProbeDoesNotFollowRedirect(t *testing.T) {
	service, config := fixture(t)
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	config.RegistryURL = redirect.URL
	report, err := service.Run(context.Background(), config)
	if err != nil || report.Healthy || report.Checks[7].Status != "fail" || hits.Load() != 0 {
		t.Fatalf("redirect followed: %+v hits=%d err=%v", report.Checks[7], hits.Load(), err)
	}
}

type canceledCommands struct{}

func (canceledCommands) Output(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type canceledResolver struct{}

func (canceledResolver) LookupIPAddr(ctx context.Context, _ string) ([]net.IPAddr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDoctorCanceledRunKeepsStableCompleteReport(t *testing.T) {
	service, config := fixture(t)
	service.Commands = canceledCommands{}
	service.Resolver = canceledResolver{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := service.Run(ctx, config)
	if err != nil || report.Healthy || report.Summary.Total != 9 || report.Summary.Passed+report.Summary.Warnings+report.Summary.Failed != 9 {
		t.Fatalf("incomplete canceled report: %+v %v", report, err)
	}
	want := []string{"network", "dns", "colima", "containerd", "kubernetes", "disk", "resources", "registry", "service:development-tools/kuchdesk-agent"}
	for i, id := range want {
		if report.Checks[i].ID != id || report.Checks[i].Status == "" || report.Checks[i].Source == "" {
			t.Fatalf("check %d missing after cancellation: %+v", i, report.Checks[i])
		}
	}
}
