package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
)

type fakePreflight struct {
	checks []doctor.Check
	err    error
	calls  int
}

func (f *fakePreflight) Run(_ context.Context, config doctor.Config) (doctor.Report, error) {
	f.calls++
	if config.Domain != "infisical.local.dev" || config.RegistryURL != "http://127.0.0.1:5001" {
		return doctor.Report{}, errors.New("bad preflight request")
	}
	if f.err != nil {
		return doctor.Report{}, f.err
	}
	checks := f.checks
	if checks == nil {
		checks = []doctor.Check{{Status: "ok"}, {Status: "ok"}, {Status: "ok"}, {Status: "ok"}, {Status: "ok"}, {Status: "ok"}, {Status: "ok"}}
	}
	return doctor.Report{Healthy: true, Checks: checks}, nil
}

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	calls   []call
	failAt  string
	digests string
	nodes   string
}

func (f *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) error {
	f.calls = append(f.calls, call{name: name, args: args})
	if f.failAt != "" && strings.HasPrefix(name+" "+strings.Join(args, " "), f.failAt) {
		return errors.New("command failed")
	}
	return nil
}

func (f *fakeRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{name: name, args: args})
	switch name + " " + args[0] {
	case "git rev-parse":
		return []byte("123456789abc\n"), nil
	case "git status":
		return []byte{}, nil
	case "docker image":
		if args[1] == "ls" {
			return nil, nil
		}
		if len(args) == 3 {
			return nil, errors.New("image absent")
		}
		return []byte(f.digests), nil
	case "crane digest":
		return []byte(f.digests), nil
	case "kubectl --context":
		return []byte(f.nodes), nil
	default:
		return nil, errors.New("unexpected output call")
	}
}

func TestHostRegistryPushAndKindPrepull(t *testing.T) {
	spec := fixture(t)
	spec.PushMode = "host-crane"
	spec.HostPushRepo = "127.0.0.1:5001/kuchdesk-agent"
	spec.KindPrepull = true
	digest := "sha256:" + strings.Repeat("b", 64)
	runner := &fakeRunner{digests: digest + "\n", nodes: `{"items":[{"metadata":{"name":"desktop-control-plane"}}]}`}
	result, err := (Workflow{Runner: runner, Preflight: &fakePreflight{}}).Apply(context.Background(), spec)
	if err != nil || result.Digest != digest {
		t.Fatalf("host push failed: %+v %v", result, err)
	}
	var save, push, staged, prepull, upgrade bool
	for _, call := range runner.calls {
		joined := call.name + " " + strings.Join(call.args, " ")
		if strings.HasPrefix(joined, "docker save ") {
			save = true
		}
		if strings.HasPrefix(joined, "crane push ") {
			push = true
		}
		if strings.Contains(joined, "docker push ") {
			t.Fatal("used Docker daemon to push through host-only port-forward")
		}
		if strings.HasPrefix(joined, "docker exec desktop-control-plane ctr -n k8s.io images pull --plain-http ") && strings.Contains(joined, spec.ImageRepository+":"+spec.ImageTag+"@"+digest) {
			staged = true
		}
		if strings.HasPrefix(joined, "docker exec desktop-control-plane crictl pull ") && strings.Contains(joined, spec.ImageRepository+":"+spec.ImageTag+"@"+digest) {
			prepull = true
		}
		if strings.HasPrefix(joined, "helm upgrade ") {
			upgrade = true
			if !strings.Contains(joined, "image.pullPolicy=Never") {
				t.Fatal("pre-pulled image may be fetched again by kubelet")
			}
		}
	}
	if !save || !push || !staged || !prepull || !upgrade {
		t.Fatalf("missing workflow phase: save=%t push=%t staged=%t prepull=%t upgrade=%t", save, push, staged, prepull, upgrade)
	}
}

func TestPrepullFailureStopsBeforeHelm(t *testing.T) {
	spec := fixture(t)
	spec.PushMode = "host-crane"
	spec.HostPushRepo = "127.0.0.1:5001/kuchdesk-agent"
	spec.KindPrepull = true
	for _, failedStep := range []string{"docker exec desktop-control-plane ctr", "docker exec desktop-control-plane crictl"} {
		runner := &fakeRunner{digests: "sha256:" + strings.Repeat("b", 64), nodes: `{"items":[{"metadata":{"name":"desktop-control-plane"}}]}`, failAt: failedStep}
		if _, err := (Workflow{Runner: runner, Preflight: &fakePreflight{}}).Apply(context.Background(), spec); err == nil {
			t.Fatalf("accepted failed node pre-pull: %s", failedStep)
		}
		for _, call := range runner.calls {
			if call.name == "helm" && call.args[0] == "upgrade" {
				t.Fatal("deployed without node image")
			}
		}
	}
}

func fixture(t *testing.T) Spec {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	chart := filepath.Join(source, "chart")
	if err := os.MkdirAll(chart, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(source, "Dockerfile"), filepath.Join(chart, "Chart.yaml"), filepath.Join(chart, "values.yaml")} {
		if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Spec{SourceDir: source, Dockerfile: filepath.Join(source, "Dockerfile"), ChartDir: chart, ValuesFiles: []string{filepath.Join(chart, "values.yaml")}, ImageRepository: "localhost:5001/kuchdesk-agent", ImageTag: "git-123456789abc", KubeContext: "docker-desktop", Namespace: "development-tools", Release: "kuchdesk-agent", Deployment: "kuchdesk-agent", Test: "go", PreflightDomain: "infisical.local.dev", PreflightRegistryURL: "http://127.0.0.1:5001"}
}

func TestApplyPinsDigestAndValidatesRollout(t *testing.T) {
	spec := fixture(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	runner := &fakeRunner{digests: `["localhost:5001/kuchdesk-agent@` + digest + `"]`}
	result, err := (Workflow{Runner: runner, Preflight: &fakePreflight{}}).Apply(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != digest || result.Image != "localhost:5001/kuchdesk-agent:git-123456789abc" {
		t.Fatalf("wrong resolved image: %+v", result)
	}
	var steps []string
	for _, call := range runner.calls {
		steps = append(steps, call.name+" "+call.args[0])
		if call.name == "helm" && (call.args[0] == "template" || call.args[0] == "upgrade") {
			joined := strings.Join(call.args, " ")
			if !strings.Contains(joined, "image.digest="+digest) || !strings.Contains(joined, "--kube-context docker-desktop") {
				t.Fatalf("Helm call lost digest or explicit context: %s", joined)
			}
		}
	}
	want := []string{"git rev-parse", "git status", "helm lint", "go test", "docker image", "docker image", "docker build", "docker push", "docker image", "helm template", "helm upgrade", "kubectl --context"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("unexpected step order: got %v want %v", steps, want)
	}
}

func TestPushFailureNeverCallsHelmUpgrade(t *testing.T) {
	spec := fixture(t)
	runner := &fakeRunner{failAt: "docker push"}
	if _, err := (Workflow{Runner: runner, Preflight: &fakePreflight{}}).Apply(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "docker push failed") {
		t.Fatalf("expected push failure, got %v", err)
	}
	for _, call := range runner.calls {
		if call.name == "helm" && call.args[0] == "upgrade" {
			t.Fatal("Helm upgraded after push failure")
		}
	}
}

func TestMissingDigestNeverCallsHelmUpgrade(t *testing.T) {
	spec := fixture(t)
	runner := &fakeRunner{digests: `[]`}
	if _, err := (Workflow{Runner: runner, Preflight: &fakePreflight{}}).Apply(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "digest unavailable") {
		t.Fatalf("expected digest failure, got %v", err)
	}
	for _, call := range runner.calls {
		if call.name == "helm" && (call.args[0] == "template" || call.args[0] == "upgrade") {
			t.Fatal("Helm ran without a resolved image digest")
		}
	}
}

func TestPreflightBlocksBeforeBuild(t *testing.T) {
	for _, preflight := range []*fakePreflight{
		{checks: []doctor.Check{{ID: "dns", Status: "fail", Message: "Configured domain does not resolve", Recommendation: "Check Technitium"}}},
		{checks: []doctor.Check{{ID: "resources", Status: "warn", Message: "Node resource use is high", Recommendation: "Check capacity"}}},
		{err: errors.New("secret-bearing internal error")},
	} {
		runner := &fakeRunner{}
		_, err := (Workflow{Runner: runner, Preflight: preflight}).Apply(context.Background(), fixture(t))
		if err == nil || preflight.calls != 1 || len(runner.calls) != 0 || strings.Contains(err.Error(), "secret-bearing") {
			t.Fatalf("unsafe preflight outcome: error=%v calls=%+v", err, runner.calls)
		}
	}
}

func TestPreflightConfigurationRequired(t *testing.T) {
	spec := fixture(t)
	spec.PreflightDomain = ""
	if _, err := spec.Plan(); err == nil {
		t.Fatal("plan accepted missing domain")
	}
	spec = fixture(t)
	spec.PreflightRegistryURL = "https://external.example"
	if _, err := spec.Plan(); err == nil {
		t.Fatal("plan accepted external registry probe")
	}
	spec = fixture(t)
	spec.PushMode = "host-crane"
	spec.HostPushRepo = "127.0.0.1:5001/kuchdesk-agent"
	spec.PreflightRegistryURL = "http://127.0.0.1:9000"
	if _, err := spec.Plan(); err == nil {
		t.Fatal("plan accepted unrelated registry port")
	}
}

func TestSpecRejectsUnsafeOrAmbiguousInputs(t *testing.T) {
	spec := fixture(t)
	spec.ImageTag = "latest"
	if _, err := spec.Plan(); err == nil {
		t.Fatal("accepted mutable image tag")
	}
	spec = fixture(t)
	spec.Dockerfile = filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(spec.Dockerfile, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Plan(); err == nil {
		t.Fatal("accepted Dockerfile outside source revision")
	}
	spec = fixture(t)
	spec.KubeContext = ""
	if _, err := spec.Plan(); err == nil {
		t.Fatal("accepted implicit Kubernetes context")
	}
}
