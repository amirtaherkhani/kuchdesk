package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	chartvalues "github.com/amirtaherkhani/kuchdesk/configyaml"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/localruntime"
)

var (
	namePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)
	contextPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,127}$`)
	imagePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]+$`)
	tagPattern      = regexp.MustCompile(`^git-[0-9a-f]{12}(-dirty-[0-9a-f]{12})?$`)
	platformPattern = regexp.MustCompile(`^linux/(amd64|arm64|arm|386|ppc64le|s390x|riscv64)(/v[0-9]+)?$`)
	buildArgPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Spec is one explicit, local deployment target. No command or shell string is
// accepted from the profile; the workflow owns the nerdctl, Helm, and kubectl
// argument lists.
type Spec struct {
	SourceDir            string            `json:"sourceDir"`
	Dockerfile           string            `json:"dockerfile"`
	ChartDir             string            `json:"chartDir"`
	ValuesFiles          []string          `json:"valuesFiles,omitempty"`
	ImageRepository      string            `json:"imageRepository"`
	ImageTag             string            `json:"imageTag"`
	KubeContext          string            `json:"kubeContext"`
	Namespace            string            `json:"namespace"`
	Release              string            `json:"release"`
	Deployment           string            `json:"deployment"`
	Test                 string            `json:"test"`
	Platform             string            `json:"platform,omitempty"`
	BuildArgs            map[string]string `json:"buildArgs,omitempty"`
	ReuseBuild           bool              `json:"reuseBuild,omitempty"`
	Rebuild              bool              `json:"rebuild,omitempty"`
	RefreshImages        bool              `json:"refreshImages,omitempty"`
	PreflightDomain      string            `json:"preflightDomain"`
	PreflightRegistryURL string            `json:"preflightRegistryUrl"`
}

type Result struct {
	Image        string `json:"image"`
	Digest       string `json:"digest"`
	SourceCommit string `json:"sourceCommit"`
	InputHash    string `json:"inputHash"`
	Platform     string `json:"platform"`
}

type Runner interface {
	Run(context.Context, string, string, ...string) error
	Output(context.Context, string, string, ...string) ([]byte, error)
}

type contextRunner struct {
	Runner
	context string
}

func (r contextRunner) Run(ctx context.Context, dir, name string, args ...string) error {
	if name == "nerdctl" {
		profile, err := localruntime.Profile(r.context)
		if err != nil {
			return err
		}
		name, args = "colima", append([]string{"--profile", profile, "nerdctl", "--", "--namespace", "k8s.io"}, args...)
	}
	if name == "ctr" {
		profile, err := localruntime.Profile(r.context)
		if err != nil {
			return err
		}
		name, args = "colima", append([]string{"--profile", profile, "ssh", "--", "sudo", "ctr", "--namespace", "k8s.io"}, args...)
	}
	return r.Runner.Run(ctx, dir, name, args...)
}

func (r contextRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if name == "nerdctl" {
		profile, err := localruntime.Profile(r.context)
		if err != nil {
			return nil, err
		}
		name, args = "colima", append([]string{"--profile", profile, "nerdctl", "--", "--namespace", "k8s.io"}, args...)
	}
	if name == "ctr" {
		profile, err := localruntime.Profile(r.context)
		if err != nil {
			return nil, err
		}
		name, args = "colima", append([]string{"--profile", profile, "ssh", "--", "sudo", "ctr", "--namespace", "k8s.io"}, args...)
	}
	return r.Runner.Output(ctx, dir, name, args...)
}

type Preflight interface {
	Run(context.Context, doctor.Config) (doctor.Report, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = filteredEnvironment()
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func (ExecRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = filteredEnvironment()
	return command.Output()
}

func filteredEnvironment() []string {
	var result []string
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "KUCHDESK_AGENT_TOKEN=") || strings.HasPrefix(item, "KUCHDESK_HOST_BRIDGE_TOKEN=") {
			continue
		}
		result = append(result, item)
	}
	return result
}

type Workflow struct {
	Runner    Runner
	OnStep    func(string)
	Preflight Preflight
}

func (s Spec) Validate() error {
	if s.ChartDir == filepath.Join(s.SourceDir, "kuchdesk-agent/chart") {
		expected := []string{filepath.Join(s.ChartDir, "values.yaml"), filepath.Join(s.ChartDir, "values.local.yaml")}
		if len(s.ValuesFiles) != len(expected) || s.ValuesFiles[0] != expected[0] || s.ValuesFiles[1] != expected[1] {
			return errors.New("Agent deployment requires its canonical base and private local YAML files")
		}
	}
	if !namePattern.MatchString(s.Namespace) || !namePattern.MatchString(s.Release) || !namePattern.MatchString(s.Deployment) || !contextPattern.MatchString(s.KubeContext) {
		return errors.New("namespace, release, deployment, and Kubernetes context are required")
	}
	if !imagePattern.MatchString(s.ImageRepository) || !tagPattern.MatchString(s.ImageTag) || strings.HasSuffix(s.ImageRepository, ":") {
		return errors.New("image repository and immutable git tag are required")
	}
	if !platformPattern.MatchString(s.Platform) {
		return errors.New("platform must name one explicit Linux OS/architecture[/variant]")
	}
	if s.ReuseBuild {
		return errors.New("containerd build reuse is unsupported without verified store provenance; set reuseBuild=false")
	}
	for key, value := range s.BuildArgs {
		if !buildArgPattern.MatchString(key) || strings.ContainsRune(value, '\x00') {
			return errors.New("build argument name or value invalid")
		}
	}
	if _, err := localruntime.Profile(s.KubeContext); err != nil {
		return err
	}
	if s.Test != "" && s.Test != "go" {
		return errors.New("only test=go or no test is supported")
	}
	if err := (doctor.Config{Domain: s.PreflightDomain, KubeContext: s.KubeContext, RegistryURL: s.PreflightRegistryURL, DiskPath: s.SourceDir}).Validate(); err != nil {
		return fmt.Errorf("preflight configuration invalid: %w", err)
	}
	for _, path := range []string{s.SourceDir, s.Dockerfile, s.ChartDir} {
		if !filepath.IsAbs(path) {
			return errors.New("sourceDir, dockerfile, and chartDir must be absolute paths")
		}
	}
	resolvedSource, sourceErr := filepath.EvalSymlinks(s.SourceDir)
	resolvedDockerfile, dockerfileErr := filepath.EvalSymlinks(s.Dockerfile)
	resolvedChart, chartErr := filepath.EvalSymlinks(s.ChartDir)
	if sourceErr != nil || dockerfileErr != nil || chartErr != nil {
		return errors.New("source, Dockerfile, or chart path cannot be resolved")
	}
	if relative, err := filepath.Rel(resolvedSource, resolvedDockerfile); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("Dockerfile must be inside the source directory")
	}
	if relative, err := filepath.Rel(resolvedSource, resolvedChart); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("chart must be inside the source directory")
	}
	for _, path := range []string{s.SourceDir, s.ChartDir} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return errors.New("source or chart directory does not exist")
		}
	}
	for _, path := range append([]string{s.Dockerfile, filepath.Join(s.ChartDir, "Chart.yaml")}, s.ValuesFiles...) {
		if !filepath.IsAbs(path) {
			return errors.New("Dockerfile and values files must use absolute paths")
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return errors.New("Dockerfile, chart, or values file does not exist")
		}
	}
	return nil
}

func (s Spec) Plan() ([]string, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	steps := []string{"doctor preflight", "validate profile and source revision", "verify Helm 4"}
	if s.Test == "go" {
		steps = append(steps, "go test")
	}
	steps = append(steps, "check local immutable image tag")
	steps = append(steps, "nerdctl build --pull")
	if s.Rebuild || s.RefreshImages {
		steps[len(steps)-1] += " --no-cache"
	}
	steps = append(steps, "resolve containerd manifest digest and verify platform/content")
	return append(steps, "helm lint", "helm template", "helm upgrade --install --rollback-on-failure --wait", "kubectl rollout status"), nil
}

// Check runs the same read-only guard used by Apply, without building or
// changing the cluster. A warning blocks deployment because required evidence
// is either missing or outside the configured resource limits.
func (w Workflow) Check(ctx context.Context, s Spec) (doctor.Report, error) {
	if err := s.Validate(); err != nil {
		return doctor.Report{}, err
	}
	preflight := w.Preflight
	if preflight == nil {
		preflight = doctor.Service{}
	}
	report, err := preflight.Run(ctx, doctor.Config{Domain: s.PreflightDomain, KubeContext: s.KubeContext, RegistryURL: s.PreflightRegistryURL, DiskPath: s.SourceDir})
	if err != nil {
		return doctor.Report{}, errors.New("doctor preflight unavailable")
	}
	for _, check := range report.Checks {
		if check.Status != "ok" {
			return report, fmt.Errorf("doctor preflight %s: %s; %s", check.ID, check.Message, check.Recommendation)
		}
	}
	if !report.Healthy || len(report.Checks) < 7 {
		return report, errors.New("doctor preflight incomplete")
	}
	return report, nil
}

func (w Workflow) Apply(ctx context.Context, s Spec) (Result, error) {
	if _, err := s.Plan(); err != nil {
		return Result{}, err
	}
	if w.Runner == nil {
		return Result{}, errors.New("command runner is required")
	}
	w.progress("doctor preflight")
	if _, err := w.Check(ctx, s); err != nil {
		return Result{}, err
	}
	w.Runner = contextRunner{Runner: w.Runner, context: s.KubeContext}
	w.progress("validate profile and source revision")
	identity, err := SourceIdentity(ctx, w.Runner, s)
	if err != nil {
		return Result{}, err
	}
	if s.ImageTag != identity.ImageTag {
		return Result{}, errors.New("image tag must match the current source/build input identity: " + identity.ImageTag)
	}
	helmVersion, err := w.Runner.Output(ctx, s.SourceDir, "helm", "version", "--short")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(helmVersion)), "v4.") {
		return Result{}, errors.New("Helm 4 is required before building or deploying")
	}
	if s.Test == "go" {
		if err := w.run(ctx, filepath.Dir(s.Dockerfile), "go test", "go", "test", "./..."); err != nil {
			return Result{}, err
		}
	}
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(ctx, s, image); err != nil {
		return Result{}, err
	}
	w.progress("resolve containerd manifest digest")
	digest, err := w.imageDigest(ctx, s, image)
	if err != nil {
		return Result{}, err
	}
	// K3s resolves a digest reference separately from the local build tag.
	if err := w.run(ctx, s.SourceDir, "register local digest reference", "ctr", "images", "tag", image, s.ImageRepository+"@"+digest); err != nil {
		return Result{}, err
	}
	current, err := SourceIdentity(ctx, w.Runner, s)
	if err != nil || current != identity {
		return Result{}, errors.New("source/build inputs changed during build; deployment stopped")
	}
	values := append(valuesArgs(s.ValuesFiles), "--set-string", "image.repository="+s.ImageRepository, "--set-string", "image.tag="+s.ImageTag, "--set-string", "image.digest="+digest)
	if s.ChartDir == filepath.Join(s.SourceDir, "kuchdesk-agent/chart") {
		for _, field := range []struct{ key, value string }{{"repository", s.ImageRepository}, {"tag", s.ImageTag}, {"digest", digest}} {
			if err := chartvalues.Set(s.SourceDir, "agent", "values.local.yaml", []string{"image", field.key}, field.value); err != nil {
				return Result{}, errors.New("cannot persist build identity in canonical Agent YAML")
			}
		}
		values = valuesArgs(s.ValuesFiles)
	}
	if err := w.run(ctx, s.SourceDir, "helm lint", "helm", append([]string{"lint", s.ChartDir}, values...)...); err != nil {
		return Result{}, err
	}
	base := []string{"--kube-context", s.KubeContext, "--namespace", s.Namespace}
	template := append(append([]string{"template", s.Release, s.ChartDir}, base...), values...)
	if err := w.run(ctx, s.SourceDir, "helm template", "helm", template...); err != nil {
		return Result{}, err
	}
	upgrade := append(append([]string{"upgrade", "--install", s.Release, s.ChartDir}, base...), values...)
	upgrade = append(upgrade, "--rollback-on-failure", "--wait", "--timeout", "5m")
	if err := w.run(ctx, s.SourceDir, "helm upgrade", "helm", upgrade...); err != nil {
		return Result{}, err
	}
	if err := w.run(ctx, s.SourceDir, "rollout status", "kubectl", "--context", s.KubeContext, "--namespace", s.Namespace, "rollout", "status", "deployment/"+s.Deployment, "--timeout=5m"); err != nil {
		return Result{}, err
	}
	return Result{Image: image, Digest: digest, SourceCommit: identity.SourceCommit, InputHash: identity.InputHash, Platform: s.Platform}, nil
}

func valuesArgs(files []string) []string {
	var args []string
	for _, file := range files {
		args = append(args, "--values", file)
	}
	return args
}

func (w Workflow) run(ctx context.Context, dir, step, name string, args ...string) error {
	w.progress(step)
	if err := w.Runner.Run(ctx, dir, name, args...); err != nil {
		return fmt.Errorf("%s failed: %w", step, err)
	}
	return nil
}

func (w Workflow) progress(step string) {
	if w.OnStep != nil {
		w.OnStep(step)
	}
}
