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
	if config.Domain != "grafana.local.dev" || config.RegistryURL != "http://127.0.0.1:5001" {
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
	if name == "colima" && len(args) >= 8 && args[2] == "ssh" {
		name, args = "ctr", args[8:]
	}
	if name == "colima" {
		if len(args) < 7 || strings.Join(args[:6], " ") != "--profile kuchdesk-fresh nerdctl -- --namespace k8s.io" {
			return errors.New("wrong containerd scope")
		}
		name, args = "nerdctl", args[6:]
	}
	f.calls = append(f.calls, call{name: name, args: args})
	if f.failAt != "" && strings.HasPrefix(name+" "+strings.Join(args, " "), f.failAt) {
		return errors.New("command failed")
	}
	return nil
}

func (f *fakeRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	if name == "colima" && len(args) >= 8 && args[2] == "ssh" {
		name, args = "ctr", args[8:]
	}
	if name == "colima" {
		if len(args) < 7 || strings.Join(args[:6], " ") != "--profile kuchdesk-fresh nerdctl -- --namespace k8s.io" {
			return nil, errors.New("wrong containerd scope")
		}
		name, args = "nerdctl", args[6:]
	}
	f.calls = append(f.calls, call{name: name, args: args})
	if f.failAt != "" && strings.HasPrefix(name+" "+strings.Join(args, " "), f.failAt) {
		return nil, errors.New("command failed")
	}
	switch name + " " + args[0] {
	case "helm version":
		return []byte("v4.3.1"), nil
	case "git rev-parse":
		return []byte("123456789abc" + strings.Repeat("0", 28) + "\n"), nil
	case "git status":
		return []byte{}, nil
	case "git ls-files":
		return nil, nil
	case "nerdctl images":
		return nil, nil
	case "nerdctl image":
		return []byte(f.digests), nil
	case "ctr images":
		return []byte("localhost:5001/kuchdesk-agent:git-123456789abc"), nil
	case "kubectl --context":
		return []byte(f.nodes), nil
	default:
		return nil, errors.New("unexpected output call")
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
	return Spec{SourceDir: source, Dockerfile: filepath.Join(source, "Dockerfile"), ChartDir: chart, ValuesFiles: []string{filepath.Join(chart, "values.yaml")}, ImageRepository: "localhost:5001/kuchdesk-agent", ImageTag: "git-123456789abc", KubeContext: "colima-kuchdesk-fresh", Namespace: "development-tools", Release: "kuchdesk-agent", Deployment: "kuchdesk-agent", Test: "go", Platform: "linux/arm64", PreflightDomain: "grafana.local.dev", PreflightRegistryURL: "http://127.0.0.1:5001"}
}

func TestApplyPinsDigestAndValidatesRollout(t *testing.T) {
	spec := fixture(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	runner := &fakeRunner{digests: nativeFixture(digest, "arm64")}
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
		if call.name == "ctr" && len(call.args) > 1 && call.args[1] == "tag" {
			if got := call.args[len(call.args)-1]; got != spec.ImageRepository+"@"+digest {
				t.Fatalf("wrong local digest reference: %s", got)
			}
		}
		if call.name == "helm" && (call.args[0] == "template" || call.args[0] == "upgrade") {
			joined := strings.Join(call.args, " ")
			if !strings.Contains(joined, "image.digest="+digest) || !strings.Contains(joined, "--kube-context colima-kuchdesk-fresh") {
				t.Fatalf("Helm call lost digest or explicit context: %s", joined)
			}
		}
	}
	want := []string{"git rev-parse", "git status", "git ls-files", "helm version", "go test", "nerdctl images", "nerdctl build", "nerdctl image", "ctr images", "ctr images", "git rev-parse", "git status", "git ls-files", "helm lint", "helm template", "helm upgrade", "kubectl --context"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("unexpected step order: got %v want %v", steps, want)
	}
}

func TestAgentDeployPersistsImageAndConsumesOnlyCanonicalYAML(t *testing.T) {
	spec := fixture(t)
	spec.ChartDir = filepath.Join(spec.SourceDir, "kuchdesk-agent/chart")
	if err := os.MkdirAll(spec.ChartDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Chart.yaml", "values.yaml"} {
		if err := os.WriteFile(filepath.Join(spec.ChartDir, name), []byte("synthetic: true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	local := filepath.Join(spec.ChartDir, "values.local.yaml")
	if err := os.WriteFile(local, []byte("image:\n  repository: ''\n  tag: ''\n  digest: ''\nunrelated: keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec.ValuesFiles = []string{filepath.Join(spec.ChartDir, "values.yaml"), local}
	digest := "sha256:" + strings.Repeat("a", 64)
	runner := &fakeRunner{digests: nativeFixture(digest, "arm64")}
	if _, err := (Workflow{Runner: runner, Preflight: &fakePreflight{}}).Apply(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(local)
	if err != nil || !strings.Contains(string(data), digest) || !strings.Contains(string(data), "unrelated: keep") {
		t.Fatal("canonical image identity not persisted safely")
	}
	for _, call := range runner.calls {
		if call.name == "helm" && call.args[0] == "upgrade" {
			args := strings.Join(call.args, " ")
			if strings.Contains(args, "--set") || !strings.Contains(args, local) || !strings.Contains(args, "--rollback-on-failure") {
				t.Fatal("Agent Helm bypassed canonical YAML")
			}
		}
	}
}

func TestBuildFailureNeverCallsHelmUpgrade(t *testing.T) {
	spec := fixture(t)
	runner := &fakeRunner{failAt: "nerdctl build"}
	if _, err := (Workflow{Runner: runner, Preflight: &fakePreflight{}}).Apply(context.Background(), spec); err == nil || !strings.Contains(err.Error(), "nerdctl build failed") {
		t.Fatalf("expected build failure, got %v", err)
	}
	for _, call := range runner.calls {
		if call.name == "helm" && call.args[0] == "upgrade" {
			t.Fatal("Helm upgraded after build failure")
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
