package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/localruntime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxCommandOutput = 1 << 20
	defaultMinDisk   = 2 << 30
)

var (
	identifier   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,127}$`)
	serviceName  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,62}$`)
	versionName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,39}$`)
	secretValue  = regexp.MustCompile(`(?i)\b(password|token|secret|api[_-]?key|authorization)\s*[:=]\s*[^\s,;]+`)
	bearerValue  = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/-]+`)
	urlValue     = regexp.MustCompile(`https?://[^\s]+`)
	addressValue = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	emailValue   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
)

// NormalizeExcerpt bounds diagnostic text and removes common credential and
// address forms. Excerpts remain untrusted source material, not instructions.
func NormalizeExcerpt(raw []byte) (string, bool) {
	clean := strings.Map(func(char rune) rune {
		if char == '\n' || char == '\t' {
			return ' '
		}
		if char < 0x20 || char == 0x7f {
			return -1
		}
		return char
	}, string(raw))
	clean = bearerValue.ReplaceAllString(clean, "Bearer [REDACTED]")
	clean = secretValue.ReplaceAllString(clean, "$1=[REDACTED]")
	clean = urlValue.ReplaceAllString(clean, "[URL]")
	clean = addressValue.ReplaceAllString(clean, "[IP]")
	clean = emailValue.ReplaceAllString(clean, "[EMAIL]")
	clean = strings.ReplaceAll(clean, "`", "′")
	clean = strings.Join(strings.Fields(clean), " ")
	runes := []rune(clean)
	if len(runes) > 512 {
		return string(runes[:512]), true
	}
	return clean, false
}

type Target struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
}

type Config struct {
	Domain       string   `json:"domain"`
	KubeContext  string   `json:"kubeContext"`
	RegistryURL  string   `json:"registryUrl"`
	DiskPath     string   `json:"diskPath"`
	MinFreeBytes uint64   `json:"minFreeBytes,omitempty"`
	Services     []Target `json:"services,omitempty"`
}

func (c Config) Validate() error {
	if !validDomain(c.Domain) || !identifier.MatchString(c.KubeContext) || !filepath.IsAbs(c.DiskPath) {
		return errors.New("domain, Kubernetes context, and absolute disk path are required")
	}
	if _, err := localruntime.Profile(c.KubeContext); err != nil {
		return err
	}
	if c.RegistryURL != "" {
		u, err := url.Parse(c.RegistryURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return errors.New("registry URL must be an HTTP origin without credentials")
		}
		if u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
			return errors.New("registry URL must use host loopback")
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("registry URL requires a valid explicit port")
		}
	}
	if len(c.Services) > 16 {
		return errors.New("at most 16 deployment checks are supported")
	}
	seen := make(map[string]bool)
	for _, service := range c.Services {
		if !serviceName.MatchString(service.Namespace) || !serviceName.MatchString(service.Deployment) {
			return errors.New("deployment targets require valid namespace and name")
		}
		key := service.Namespace + "/" + service.Deployment
		if seen[key] {
			return errors.New("duplicate deployment target")
		}
		seen[key] = true
	}
	return nil
}

func validDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}

type Check struct {
	ID             string         `json:"id"`
	Component      string         `json:"component"`
	Status         string         `json:"status"`
	Message        string         `json:"message"`
	Evidence       map[string]any `json:"evidence,omitempty"`
	Recommendation string         `json:"recommendation,omitempty"`
	Source         string         `json:"source"`
	Context        string         `json:"context"`
	ObservedAt     time.Time      `json:"observedAt"`
	DurationMS     int64          `json:"durationMs"`
	ExitCode       *int           `json:"exitCode,omitempty"`
	ErrorClass     string         `json:"errorClass,omitempty"`
	Version        string         `json:"version,omitempty"`
	RawExcerpt     string         `json:"rawExcerpt,omitempty"`
	Truncated      bool           `json:"truncated"`
	Dependencies   []string       `json:"dependencies,omitempty"`
}

type Finding struct {
	CheckID        string         `json:"checkId"`
	Severity       string         `json:"severity"`
	Problem        string         `json:"problem"`
	Evidence       map[string]any `json:"evidence,omitempty"`
	Recommendation string         `json:"recommendation"`
}

type Summary struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Failed   int `json:"failed"`
}

type Report struct {
	SchemaVersion string    `json:"schemaVersion"`
	GeneratedAt   time.Time `json:"generatedAt"`
	DurationMS    int64     `json:"durationMs"`
	Healthy       bool      `json:"healthy"`
	Summary       Summary   `json:"summary"`
	Checks        []Check   `json:"checks"`
	Findings      []Finding `json:"findings"`
	Limitations   []string  `json:"limitations"`
}

type CommandRunner interface {
	Output(context.Context, string, ...string) ([]byte, error)
}

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type Service struct {
	Commands CommandRunner
	Resolver Resolver
	HTTP     *http.Client
	DiskFree func(string) (uint64, error)
}

type execRunner struct{}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = filteredEnvironment()
	result, err := command.Output()
	if len(result) > maxCommandOutput {
		return nil, errors.New("command response exceeds limit")
	}
	return result, err
}

func filteredEnvironment() []string {
	var result []string
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "KUCHDESK_HOST_BRIDGE_TOKEN=") || strings.HasPrefix(item, "KUCHDESK_AGENT_TOKEN=") {
			continue
		}
		result = append(result, item)
	}
	return result
}

func freeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}

func (s Service) withDefaults() Service {
	if s.Commands == nil {
		s.Commands = execRunner{}
	}
	if s.Resolver == nil {
		s.Resolver = net.DefaultResolver
	}
	if s.HTTP == nil {
		s.HTTP = &http.Client{Timeout: 5 * time.Second}
	}
	if s.DiskFree == nil {
		s.DiskFree = freeBytes
	}
	return s
}

func (s Service) Run(ctx context.Context, config Config) (Report, error) {
	if err := config.Validate(); err != nil {
		return Report{}, err
	}
	started := time.Now()
	s = s.withDefaults()
	if config.MinFreeBytes == 0 {
		config.MinFreeBytes = defaultMinDisk
	}
	type task struct {
		id, component, source, context string
		dependencies                   []string
		run                            func(context.Context) Check
	}
	tasks := []task{
		{"network", "host-network", "route", "macos", nil, func(ctx context.Context) Check { return s.network(ctx, config.Domain) }},
		{"dns", "host-dns", "system-resolver", "macos", []string{"network"}, func(ctx context.Context) Check { return s.dns(ctx, config.Domain) }},
		{"colima", "colima", "colima-status", config.KubeContext, nil, func(ctx context.Context) Check { return s.colima(ctx, config.KubeContext) }},
		{"containerd", "containerd", "colima-nerdctl-info", config.KubeContext, []string{"colima"}, func(ctx context.Context) Check { return s.containerd(ctx, config.KubeContext) }},
		{"kubernetes", "kubernetes", "kubectl-get-nodes", config.KubeContext, []string{"containerd"}, func(ctx context.Context) Check { return s.kubernetes(ctx, config.KubeContext) }},
		{"disk", "storage", "statfs", "macos", nil, func(context.Context) Check { return s.disk(config.DiskPath, config.MinFreeBytes) }},
		{"resources", "kubernetes-resources", "kubectl-top-nodes", config.KubeContext, []string{"kubernetes"}, func(ctx context.Context) Check { return s.metrics(ctx, config.KubeContext) }},
	}
	if config.RegistryURL != "" {
		tasks = append(tasks, task{"registry", "registry", "registry-http", "loopback", []string{"containerd", "kubernetes"}, func(ctx context.Context) Check { return s.registry(ctx, config.RegistryURL) }})
	}
	for _, target := range config.Services {
		target := target
		tasks = append(tasks, task{"service:" + target.Namespace + "/" + target.Deployment, "deployment", "kubectl-get-deployment", config.KubeContext, []string{"kubernetes"}, func(ctx context.Context) Check { return s.deployment(ctx, config.KubeContext, target) }})
	}
	checks := make([]Check, len(tasks))
	gate := make(chan struct{}, 4)
	var workers sync.WaitGroup
	for index, task := range tasks {
		workers.Add(1)
		go func() {
			defer workers.Done()
			checkStarted := time.Now()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				checks[index] = failure(task.id, "Doctor check canceled", "Run Doctor again when the request can complete")
				checks[index].ErrorClass = "request_canceled"
				checks[index].Component, checks[index].Source, checks[index].Context = task.component, task.source, task.context
				checks[index].ObservedAt, checks[index].DurationMS = time.Now().UTC(), time.Since(checkStarted).Milliseconds()
				checks[index].Dependencies = task.dependencies
				return
			}
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			check := task.run(checkCtx)
			check.Component, check.Source, check.Context = task.component, task.source, task.context
			check.ObservedAt, check.DurationMS = time.Now().UTC(), time.Since(checkStarted).Milliseconds()
			check.Dependencies = task.dependencies
			checks[index] = check
		}()
	}
	workers.Wait()
	report := Report{SchemaVersion: "kuchdesk.doctor/v2", GeneratedAt: time.Now().UTC(), DurationMS: time.Since(started).Milliseconds(), Healthy: true, Checks: checks, Findings: []Finding{}, Limitations: []string{"Point-in-time observations only; Kubernetes authorizes every later action again.", "No arbitrary process, firewall, router, or Secret inspection is performed."}}
	report.Summary.Total = len(checks)
	for _, check := range checks {
		switch check.Status {
		case "ok":
			report.Summary.Passed++
		case "warn":
			report.Summary.Warnings++
		default:
			report.Summary.Failed++
			report.Healthy = false
		}
		if check.Status != "ok" {
			report.Findings = append(report.Findings, Finding{CheckID: check.ID, Severity: check.Status, Problem: check.Message, Evidence: check.Evidence, Recommendation: check.Recommendation})
		}
	}
	return report, nil
}

func ok(id, message string, evidence map[string]any) Check {
	return Check{ID: id, Status: "ok", Message: message, Evidence: evidence}
}
func failure(id, message, recommendation string) Check {
	return Check{ID: id, Status: "fail", Message: message, Recommendation: recommendation}
}
func warning(id, message, recommendation string) Check {
	return Check{ID: id, Status: "warn", Message: message, Recommendation: recommendation}
}

func commandExitCode(err error) *int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code := exit.ExitCode()
		return &code
	}
	return nil
}

func (s Service) network(ctx context.Context, domain string) Check {
	addresses, err := s.Resolver.LookupIPAddr(ctx, domain)
	if err != nil || len(addresses) == 0 {
		check := failure("network", "Configured ingress route unavailable", "Check the configured domain and its host route")
		check.ErrorClass = "route_target_unavailable"
		return check
	}
	interfaces := make(map[string]bool)
	var excerpts []byte
	for _, address := range addresses {
		raw, routeErr := s.Commands.Output(ctx, "route", "-n", "get", address.IP.String())
		if routeErr != nil {
			check := failure("network", "Configured ingress route unavailable", "Check the host route to the configured domain")
			check.ErrorClass = "command_failed"
			check.ExitCode = commandExitCode(routeErr)
			return check
		}
		excerpts = append(excerpts, raw...)
		found := false
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) == 2 && fields[0] == "interface:" && serviceName.MatchString(fields[1]) {
				interfaces[fields[1]] = true
				found = true
				break
			}
		}
		if !found {
			check := failure("network", "Configured ingress route interface unknown", "Inspect the host route to the configured domain")
			check.ErrorClass, check.RawExcerpt, check.Truncated = "parse_error", "route interface unavailable", false
			return check
		}
	}
	for iface := range interfaces {
		if strings.HasPrefix(iface, "utun") {
			excerpt, truncated := NormalizeExcerpt(excerpts)
			evidence := map[string]any{"interface": iface}
			if len(interfaces) > 1 {
				evidence = map[string]any{"interfaces": interfaces}
			}
			return Check{ID: "network", Status: "warn", Message: "Configured ingress route uses a VPN interface", Evidence: evidence, Recommendation: "Verify the local ingress route and VPN split routing", ErrorClass: "vpn_route", RawExcerpt: excerpt, Truncated: truncated}
		}
	}
	check := ok("network", "Configured ingress route found", map[string]any{"interfaces": interfaces})
	check.RawExcerpt, check.Truncated = NormalizeExcerpt(excerpts)
	return check
}

func (s Service) dns(ctx context.Context, domain string) Check {
	addresses, err := s.Resolver.LookupIPAddr(ctx, domain)
	if err != nil || len(addresses) == 0 {
		check := failure("dns", "Configured domain does not resolve", "Check Technitium and the host DNS resolver")
		check.ErrorClass = "dns_lookup_failed"
		return check
	}
	return ok("dns", "Configured domain resolves", map[string]any{"answerCount": len(addresses)})
}

func (s Service) colima(ctx context.Context, kubeContext string) Check {
	profile, err := localruntime.Profile(kubeContext)
	if err != nil {
		return failure("colima", "Colima context invalid", "Select an explicit Colima context")
	}
	if _, err := s.Commands.Output(ctx, "colima", "--profile", profile, "status"); err != nil {
		return failure("colima", "Colima profile is not running", "Start the approved profile only after deployment authorization")
	}
	return ok("colima", "Selected Colima profile is running", map[string]any{"profile": profile})
}

func (s Service) containerd(ctx context.Context, kubeContext string) Check {
	profile, _ := localruntime.Profile(kubeContext)
	raw, err := s.Commands.Output(ctx, "colima", "--profile", profile, "nerdctl", "--", "--namespace", "k8s.io", "info", "--format", "{{json .}}")
	var info struct {
		Driver        string `json:"Driver"`
		ServerVersion string `json:"ServerVersion"`
	}
	if err != nil || len(raw) > maxCommandOutput || json.Unmarshal(raw, &info) != nil || info.Driver == "" || !versionName.MatchString(info.ServerVersion) {
		check := failure("containerd", "Selected Colima containerd store unavailable", "Check the containerd runtime in the approved Colima profile")
		check.ErrorClass, check.ExitCode = "command_or_parse_failed", commandExitCode(err)
		return check
	}
	check := ok("containerd", "Selected Colima containerd k8s.io store responds", map[string]any{"serverVersion": info.ServerVersion, "namespace": "k8s.io"})
	check.Version = info.ServerVersion
	return check
}

func (s Service) kubernetes(ctx context.Context, kubeContext string) Check {
	raw, err := s.Commands.Output(ctx, "kubectl", "--context", kubeContext, "get", "nodes", "-o", "json")
	var body struct {
		Items []struct {
			Status struct {
				NodeInfo struct {
					KubeletVersion          string `json:"kubeletVersion"`
					ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
					MachineID               string `json:"machineID"`
				} `json:"nodeInfo"`
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err != nil || len(raw) > maxCommandOutput || json.Unmarshal(raw, &body) != nil || len(body.Items) == 0 {
		check := failure("kubernetes", "Kubernetes nodes unavailable", "Check the selected Kubernetes context")
		check.ErrorClass, check.ExitCode = "command_or_parse_failed", commandExitCode(err)
		return check
	}
	ready := 0
	profile, _ := localruntime.Profile(kubeContext)
	machine, machineErr := s.Commands.Output(ctx, "colima", "--profile", profile, "ssh", "--", "cat", "/etc/machine-id")
	if machineErr != nil || strings.TrimSpace(string(machine)) == "" || len(body.Items) != 1 || body.Items[0].Status.NodeInfo.MachineID != strings.TrimSpace(string(machine)) {
		return failure("kubernetes", "Kubernetes context does not identify the selected single-node Colima VM", "Verify the context and VM identity before deployment")
	}
	for _, node := range body.Items {
		if !strings.Contains(node.Status.NodeInfo.KubeletVersion, "+k3s") || !strings.HasPrefix(node.Status.NodeInfo.ContainerRuntimeVersion, "containerd://") {
			return failure("kubernetes", "Selected cluster is not K3s with Colima containerd runtime", "Use the dedicated Colima containerd/K3s profile")
		}
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				ready++
				break
			}
		}
	}
	evidence := map[string]any{"nodes": len(body.Items), "readyNodes": ready}
	if ready != len(body.Items) {
		return Check{ID: "kubernetes", Status: "fail", Message: "Kubernetes has unready nodes", Evidence: evidence, Recommendation: "Inspect node conditions before deployment", ErrorClass: "node_unready"}
	}
	return ok("kubernetes", "Kubernetes nodes ready", evidence)
}

func (s Service) registry(ctx context.Context, origin string) Check {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(origin, "/")+"/v2/", nil)
	if err != nil {
		return failure("registry", "Registry URL invalid", "Correct the registry origin")
	}
	client := *s.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		check := failure("registry", "Local registry unreachable", "Check the registry Deployment and host port-forward")
		check.ErrorClass = "transport_error"
		return check
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Check{ID: "registry", Status: "fail", Message: "Local registry rejected the probe", Evidence: map[string]any{"httpStatus": response.StatusCode}, Recommendation: "Check registry authentication and host forwarding", ErrorClass: "http_status"}
	}
	return ok("registry", "Local registry responds", map[string]any{"httpStatus": response.StatusCode})
}

func (s Service) disk(path string, minimum uint64) Check {
	free, err := s.DiskFree(path)
	if err != nil {
		check := failure("disk", "Free disk space unavailable", "Check the source volume and filesystem")
		check.ErrorClass = "statfs_failed"
		return check
	}
	evidence := map[string]any{"freeGiB": free / (1 << 30), "minimumGiB": minimum / (1 << 30)}
	if free < minimum {
		return Check{ID: "disk", Status: "fail", Message: "Insufficient free disk space", Evidence: evidence, Recommendation: "Free space before building or pulling images", ErrorClass: "low_disk"}
	}
	return ok("disk", "Disk space sufficient", evidence)
}

func (s Service) metrics(ctx context.Context, kubeContext string) Check {
	raw, err := s.Commands.Output(ctx, "kubectl", "--context", kubeContext, "top", "nodes", "--no-headers")
	if err != nil || len(raw) > maxCommandOutput {
		check := warning("resources", "Node metrics unavailable", "Check metrics-server before resource-sensitive deployment")
		check.ErrorClass, check.ExitCode = "metrics_unavailable", commandExitCode(err)
		return check
	}
	excerpt, truncated := NormalizeExcerpt(raw)
	count, maxCPU, maxMemory := 0, 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 {
			check := warning("resources", "Node metrics unavailable", "Check metrics-server before resource-sensitive deployment")
			check.ErrorClass, check.RawExcerpt, check.Truncated = "metrics_parse_failed", excerpt, truncated
			return check
		}
		cpu, cpuErr := strconv.Atoi(strings.TrimSuffix(fields[2], "%"))
		memory, memoryErr := strconv.Atoi(strings.TrimSuffix(fields[4], "%"))
		if cpuErr != nil || memoryErr != nil || !strings.HasSuffix(fields[2], "%") || !strings.HasSuffix(fields[4], "%") || cpu < 0 || cpu > 100 || memory < 0 || memory > 100 {
			check := warning("resources", "Node metrics unavailable", "Check metrics-server before resource-sensitive deployment")
			check.ErrorClass, check.RawExcerpt, check.Truncated = "metrics_parse_failed", excerpt, truncated
			return check
		}
		count++
		maxCPU = max(maxCPU, cpu)
		maxMemory = max(maxMemory, memory)
	}
	if count == 0 {
		check := warning("resources", "Node metrics unavailable", "Check metrics-server before resource-sensitive deployment")
		check.ErrorClass = "metrics_empty"
		return check
	}
	evidence := map[string]any{"metricNodes": count, "maxCPUPercent": maxCPU, "maxMemoryPercent": maxMemory}
	if maxCPU >= 95 || maxMemory >= 95 {
		return Check{ID: "resources", Status: "fail", Message: "Node resource use is critical", Evidence: evidence, Recommendation: "Reduce load or add capacity before deployment", ErrorClass: "node_pressure", RawExcerpt: excerpt, Truncated: truncated}
	}
	if maxCPU >= 85 || maxMemory >= 85 {
		return Check{ID: "resources", Status: "warn", Message: "Node resource use is high", Evidence: evidence, Recommendation: "Check node capacity before deployment", ErrorClass: "node_pressure", RawExcerpt: excerpt, Truncated: truncated}
	}
	check := ok("resources", "Node resource use within limits", evidence)
	check.RawExcerpt, check.Truncated = excerpt, truncated
	return check
}

func (s Service) deployment(ctx context.Context, kubeContext string, target Target) Check {
	id := "service:" + target.Namespace + "/" + target.Deployment
	raw, err := s.Commands.Output(ctx, "kubectl", "--context", kubeContext, "-n", target.Namespace, "get", "deployment", target.Deployment, "-o", "json")
	var body struct {
		Spec struct {
			Replicas *int `json:"replicas"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
	}
	if err != nil || len(raw) > maxCommandOutput || json.Unmarshal(raw, &body) != nil {
		check := failure(id, "Deployment unavailable", "Inspect the named Kubernetes Deployment")
		check.ErrorClass, check.ExitCode = "command_or_parse_failed", commandExitCode(err)
		return check
	}
	desired := 1
	if body.Spec.Replicas != nil {
		desired = *body.Spec.Replicas
	}
	evidence := map[string]any{"desired": desired, "ready": body.Status.ReadyReplicas}
	if desired == 0 || body.Status.ReadyReplicas < desired {
		return Check{ID: id, Status: "fail", Message: "Deployment not ready", Evidence: evidence, Recommendation: "Inspect Pods, events, and rollout status", ErrorClass: "deployment_unready"}
	}
	return ok(id, "Deployment ready", evidence)
}
