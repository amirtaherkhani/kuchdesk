package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
)

var (
	namePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)
	contextPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,127}$`)
	imagePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]+$`)
	tagPattern      = regexp.MustCompile(`^git-[0-9a-f]{12}$`)
	platformPattern = regexp.MustCompile(`^linux/(amd64|arm64|arm|386|ppc64le|s390x|riscv64)(/v[0-9]+)?$`)
	buildArgPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	digestPattern   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Spec is one explicit, local deployment target. No command or shell string is
// accepted from the profile; the workflow owns the Docker, Helm, and kubectl
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
	PushMode             string            `json:"pushMode,omitempty"`
	HostPushRepo         string            `json:"hostPushRepository,omitempty"`
	KindPrepull          bool              `json:"kindPrepull,omitempty"`
	Platform             string            `json:"platform,omitempty"`
	BuildArgs            map[string]string `json:"buildArgs,omitempty"`
	ReuseBuild           bool              `json:"reuseBuild,omitempty"`
	Rebuild              bool              `json:"rebuild,omitempty"`
	RefreshImages        bool              `json:"refreshImages,omitempty"`
	PreflightDomain      string            `json:"preflightDomain"`
	PreflightRegistryURL string            `json:"preflightRegistryUrl"`
}

type Result struct {
	Image  string `json:"image"`
	Digest string `json:"digest"`
}

type Runner interface {
	Run(context.Context, string, string, ...string) error
	Output(context.Context, string, string, ...string) ([]byte, error)
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
		if strings.HasPrefix(item, "INFISICAL_CLIENT_ID=") || strings.HasPrefix(item, "INFISICAL_CLIENT_SECRET=") {
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
	if !namePattern.MatchString(s.Namespace) || !namePattern.MatchString(s.Release) || !namePattern.MatchString(s.Deployment) || !contextPattern.MatchString(s.KubeContext) {
		return errors.New("namespace, release, deployment, and Kubernetes context are required")
	}
	if !imagePattern.MatchString(s.ImageRepository) || !tagPattern.MatchString(s.ImageTag) || strings.HasSuffix(s.ImageRepository, ":") {
		return errors.New("image repository and immutable git tag are required")
	}
	if s.Platform != "" && !platformPattern.MatchString(s.Platform) {
		return errors.New("platform must name one explicit Linux OS/architecture[/variant]")
	}
	for key, value := range s.BuildArgs {
		if !buildArgPattern.MatchString(key) || strings.ContainsRune(value, '\x00') {
			return errors.New("build argument name or value invalid")
		}
	}
	if s.PushMode != "" && s.PushMode != "docker" && s.PushMode != "host-crane" {
		return errors.New("pushMode must be docker or host-crane")
	}
	if s.PushMode == "host-crane" {
		parts := strings.SplitN(s.HostPushRepo, "/", 2)
		if len(parts) != 2 || (parts[0] != "127.0.0.1:5001" && parts[0] != "localhost:5001") || !imagePattern.MatchString(s.HostPushRepo) || parts[1] != repositoryPath(s.ImageRepository) {
			return errors.New("hostPushRepository must use loopback port 5001 and the same image repository path")
		}
	} else if s.HostPushRepo != "" {
		return errors.New("hostPushRepository requires pushMode=host-crane")
	}
	if s.KindPrepull && !(strings.HasPrefix(s.ImageRepository, "localhost:5001/") || strings.HasPrefix(s.ImageRepository, "127.0.0.1:5001/")) {
		return errors.New("kindPrepull requires a node-local HTTP registry on port 5001")
	}
	if s.Test != "" && s.Test != "go" {
		return errors.New("only test=go or no test is supported")
	}
	if err := (doctor.Config{Domain: s.PreflightDomain, KubeContext: s.KubeContext, RegistryURL: s.PreflightRegistryURL, DiskPath: s.SourceDir}).Validate(); err != nil {
		return fmt.Errorf("preflight configuration invalid: %w", err)
	}
	if s.PushMode == "host-crane" && s.PreflightRegistryURL != "http://127.0.0.1:5001" && s.PreflightRegistryURL != "http://localhost:5001" {
		return errors.New("host-crane preflight must check the local registry on port 5001")
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
	steps := []string{"doctor preflight", "validate profile and source revision", "helm lint"}
	if s.Test == "go" {
		steps = append(steps, "go test")
	}
	steps = append(steps, "check local immutable image tag")
	if s.ReuseBuild {
		steps = append(steps, "reuse eligible Docker build or docker build --pull")
	} else {
		steps = append(steps, "docker build --pull")
	}
	if s.Rebuild || s.RefreshImages {
		steps[len(steps)-1] += " --no-cache"
	}
	if s.PushMode == "host-crane" {
		steps = append(steps, "docker save", "crane push", "crane digest")
	} else {
		steps = append(steps, "docker push", "resolve image digest")
	}
	if s.KindPrepull {
		steps = append(steps, "discover KIND nodes", "fetch digest through KIND containerd", "register digest with KIND CRI")
	}
	return append(steps, "helm template", "helm upgrade --install --atomic --wait", "kubectl rollout status"), nil
}

func repositoryPath(repository string) string {
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
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
	w.progress("validate profile and source revision")
	// A git tag must identify exactly the clean source being built.
	revision, err := w.Runner.Output(ctx, s.SourceDir, "git", "rev-parse", "--short=12", "HEAD")
	if err != nil || s.ImageTag != "git-"+strings.TrimSpace(string(revision)) {
		return Result{}, errors.New("image tag must match the source HEAD")
	}
	changed, err := w.Runner.Output(ctx, s.SourceDir, "git", "status", "--porcelain", "--untracked-files=all", "--", ".")
	if err != nil || len(strings.TrimSpace(string(changed))) != 0 {
		return Result{}, errors.New("source has uncommitted or untracked files")
	}
	lintValues := append(valuesArgs(s.ValuesFiles), "--set-string", "image.repository="+s.ImageRepository, "--set-string", "image.tag="+s.ImageTag)
	if err := w.run(ctx, s.SourceDir, "helm lint", "helm", append([]string{"lint", s.ChartDir}, lintValues...)...); err != nil {
		return Result{}, err
	}
	if s.Test == "go" {
		if err := w.run(ctx, s.SourceDir, "go test", "go", "test", "./..."); err != nil {
			return Result{}, err
		}
	}
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(ctx, s, image); err != nil {
		return Result{}, err
	}
	var digest string
	if s.PushMode == "host-crane" {
		archive, err := os.CreateTemp("", "kuchdesk-image-*.tar")
		if err != nil {
			return Result{}, errors.New("cannot create temporary image archive")
		}
		archive.Close()
		defer os.Remove(archive.Name())
		if err := w.run(ctx, s.SourceDir, "docker save", "docker", "save", "--output", archive.Name(), image); err != nil {
			return Result{}, err
		}
		if err := w.run(ctx, s.SourceDir, "crane push", "crane", "push", "--insecure", archive.Name(), s.HostPushRepo+":"+s.ImageTag); err != nil {
			return Result{}, err
		}
		w.progress("crane digest")
		raw, err := w.Runner.Output(ctx, s.SourceDir, "crane", "digest", "--insecure", s.HostPushRepo+":"+s.ImageTag)
		if err != nil {
			return Result{}, errors.New("host registry digest lookup failed")
		}
		digest = strings.TrimSpace(string(raw))
	} else {
		if err := w.run(ctx, s.SourceDir, "docker push", "docker", "push", image); err != nil {
			return Result{}, err
		}
		w.progress("resolve image digest")
		raw, err := w.Runner.Output(ctx, s.SourceDir, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", image)
		if err != nil {
			return Result{}, errors.New("image digest lookup failed")
		}
		var repoDigests []string
		if json.Unmarshal(raw, &repoDigests) != nil {
			return Result{}, errors.New("image digest response invalid")
		}
		for _, candidate := range repoDigests {
			if strings.HasPrefix(candidate, s.ImageRepository+"@") {
				digest = strings.TrimPrefix(candidate, s.ImageRepository+"@")
				break
			}
		}
	}
	if !digestPattern.MatchString(digest) {
		return Result{}, errors.New("pushed image digest unavailable")
	}
	if s.KindPrepull {
		if err := w.prepullKind(ctx, s, image+"@"+digest); err != nil {
			return Result{}, err
		}
	}
	values := append(valuesArgs(s.ValuesFiles), "--set-string", "image.repository="+s.ImageRepository, "--set-string", "image.tag="+s.ImageTag, "--set-string", "image.digest="+digest)
	if s.KindPrepull {
		values = append(values, "--set-string", "image.pullPolicy=Never")
	}
	base := []string{"--kube-context", s.KubeContext, "--namespace", s.Namespace}
	template := append(append([]string{"template", s.Release, s.ChartDir}, base...), values...)
	if err := w.run(ctx, s.SourceDir, "helm template", "helm", template...); err != nil {
		return Result{}, err
	}
	upgrade := append(append([]string{"upgrade", "--install", s.Release, s.ChartDir}, base...), values...)
	upgrade = append(upgrade, "--atomic", "--wait", "--timeout", "5m")
	if err := w.run(ctx, s.SourceDir, "helm upgrade", "helm", upgrade...); err != nil {
		return Result{}, err
	}
	if err := w.run(ctx, s.SourceDir, "rollout status", "kubectl", "--context", s.KubeContext, "--namespace", s.Namespace, "rollout", "status", "deployment/"+s.Deployment, "--timeout=5m"); err != nil {
		return Result{}, err
	}
	return Result{Image: image, Digest: digest}, nil
}

func (w Workflow) prepullKind(ctx context.Context, spec Spec, image string) error {
	w.progress("discover KIND nodes")
	raw, err := w.Runner.Output(ctx, spec.SourceDir, "kubectl", "--context", spec.KubeContext, "get", "nodes", "-o", "json")
	if err != nil {
		return errors.New("KIND node discovery failed")
	}
	var nodes struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &nodes) != nil || len(nodes.Items) == 0 || len(nodes.Items) > 16 {
		return errors.New("KIND node list invalid or exceeds 16 nodes")
	}
	for _, node := range nodes.Items {
		name := node.Metadata.Name
		if !namePattern.MatchString(name) {
			return errors.New("KIND node name invalid")
		}
		if err := w.ensureKindImage(ctx, spec, name, image); err != nil {
			return err
		}
	}
	return nil
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
