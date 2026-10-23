package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/deploy"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
)

type healthyPreflight struct{}

func (healthyPreflight) Run(context.Context, doctor.Config) (doctor.Report, error) {
	checks := make([]doctor.Check, 7)
	for i := range checks {
		checks[i].Status = "ok"
	}
	return doctor.Report{Healthy: true, Checks: checks}, nil
}

type countingRunner struct{ calls int }

func (r *countingRunner) Run(context.Context, string, string, ...string) error {
	r.calls++
	return nil
}
func (r *countingRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	r.calls++
	if name == "helm" && args[0] == "version" {
		return []byte("v4.3.1"), nil
	}
	if name == "colima" && len(args) > 8 && args[2] == "ssh" {
		return []byte("localhost:5001/kuchdesk-agent:git-123456789abc"), nil
	}
	if name == "colima" {
		name, args = "nerdctl", args[6:]
	}
	if name == "git" && args[0] == "rev-parse" {
		return []byte("123456789abc" + strings.Repeat("0", 28) + "\n"), nil
	}
	if name == "git" && args[0] == "status" {
		return nil, nil
	}
	if name == "git" && args[0] == "ls-files" {
		return nil, nil
	}
	if name == "nerdctl" && args[0] == "images" {
		return nil, nil
	}
	return []byte(`[{"Image":{"target":{"digest":"sha256:` + strings.Repeat("a", 64) + `"}},"ImageConfig":{"os":"linux","architecture":"arm64"}}]`), nil
}

func testProfile(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	chart := filepath.Join(source, "chart")
	if err := os.MkdirAll(chart, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(source, "Dockerfile"), filepath.Join(chart, "Chart.yaml")} {
		if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	spec := deploy.Spec{SourceDir: source, Dockerfile: filepath.Join(source, "Dockerfile"), ChartDir: chart, ImageRepository: "localhost:5001/kuchdesk-agent", ImageTag: "git-123456789abc", KubeContext: "colima-kuchdesk-fresh", Namespace: "development-tools", Release: "kuchdesk-agent", Deployment: "kuchdesk-agent", Test: "go", Platform: "linux/arm64", PreflightDomain: "grafana.local.dev", PreflightRegistryURL: "http://127.0.0.1:5001"}
	profile := filepath.Join(root, "profile.json")
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestPlanAndConfirmationBoundary(t *testing.T) {
	profile := testProfile(t)
	runner := &countingRunner{}
	var out, errOut bytes.Buffer
	if code := run([]string{"-profile", profile}, &out, &errOut, runner); code != 0 || runner.calls != 0 || !strings.Contains(out.String(), "helm upgrade") {
		t.Fatalf("plan executed a command: code=%d calls=%d out=%q err=%q", code, runner.calls, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := runWithPreflight([]string{"-profile", profile, "-preflight"}, &out, &errOut, runner, healthyPreflight{}); code != 0 || runner.calls != 0 || !strings.Contains(out.String(), `"healthy":true`) {
		t.Fatalf("preflight changed state: code=%d calls=%d out=%q err=%q", code, runner.calls, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"-profile", profile, "-apply", "-confirm", "wrong/namespace"}, &out, &errOut, runner); code != 2 || runner.calls != 0 {
		t.Fatalf("mismatched confirmation was accepted: code=%d calls=%d", code, runner.calls)
	}
	out.Reset()
	errOut.Reset()
	if code := runWithPreflight([]string{"-profile", profile, "-apply", "-confirm", "kuchdesk-agent/development-tools"}, &out, &errOut, runner, healthyPreflight{}); code != 0 || runner.calls == 0 || !strings.Contains(out.String(), "sha256:") {
		t.Fatalf("confirmed workflow failed: code=%d calls=%d out=%q err=%q", code, runner.calls, out.String(), errOut.String())
	}
}

func TestIdentityIsReadOnlyAndReportsFullInputIdentity(t *testing.T) {
	profile := testProfile(t)
	runner := &countingRunner{}
	var out, errOut bytes.Buffer
	if code := run([]string{"-profile", profile, "-identity"}, &out, &errOut, runner); code != 0 {
		t.Fatalf("identity failed: %s", errOut.String())
	}
	var identity deploy.Identity
	if json.Unmarshal(out.Bytes(), &identity) != nil || len(identity.SourceCommit) != 40 || len(identity.InputHash) != 64 || identity.ImageTag != "git-123456789abc" {
		t.Fatalf("invalid identity: %s", out.String())
	}
	if runner.calls != 3 {
		t.Fatalf("identity executed commands beyond three Git reads: %d", runner.calls)
	}
}
