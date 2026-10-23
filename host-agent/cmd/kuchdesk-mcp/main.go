package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/amirtaherkhani/kuchdesk/configyaml"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/async"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/deploy"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/serviceaccess"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var releaseVersion = "dev"

type buildIdentity struct {
	Version        string `json:"version"`
	SourceCommit   string `json:"sourceCommit,omitempty"`
	SourceModified bool   `json:"sourceModified"`
}

func currentBuildIdentity() buildIdentity {
	identity := buildIdentity{Version: releaseVersion}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				identity.SourceCommit = setting.Value
			case "vcs.modified":
				identity.SourceModified = setting.Value == "true"
			}
		}
	}
	return identity
}

type capabilityInput struct{}
type serviceAccessOutput struct {
	Report map[string]any `json:"report"`
}
type capabilityOutput struct {
	Build      buildIdentity        `json:"build"`
	Deployment deploymentCapability `json:"deployment"`
}
type deploymentCapability struct {
	Enabled                    bool `json:"enabled"`
	ProfileDirectoryConfigured bool `json:"profileDirectoryConfigured"`
	ProfileAvailable           bool `json:"profileAvailable"`
}
type jobInput struct {
	ID string `json:"id" jsonschema:"required,operation ID"`
}
type jobStarted struct {
	ID string `json:"id"`
}
type jobOutput struct {
	ID         string         `json:"id"`
	State      async.State    `json:"state"`
	Deployment *deploy.Result `json:"deployment,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type deployConfig struct {
	ProfileDir  string
	ProjectRoot string
	Enabled     bool
	Runner      deploy.Runner
	Preflight   deploy.Preflight
}

type valuesInput struct {
	Service string `json:"service" jsonschema:"required,service name from the chart values catalog"`
	File    string `json:"file" jsonschema:"required,canonical values filename"`
}

type valuesEditInput struct {
	Service   string   `json:"service" jsonschema:"required,service name from the chart values catalog"`
	File      string   `json:"file" jsonschema:"required,canonical values filename"`
	Keys      []string `json:"keys" jsonschema:"required,path of existing YAML keys or sequence indexes"`
	ValueYAML string   `json:"valueYaml" jsonschema:"required,one YAML value; never returned by this tool"`
}

type valuesOutput struct {
	YAML string `json:"yaml"`
}

type valuesEdited struct {
	Changed bool `json:"changed"`
}

type deployInput struct {
	Profile string `json:"profile" jsonschema:"required,profile filename within configured directory"`
}

type deployStartInput struct {
	Profile string `json:"profile" jsonschema:"required,profile filename within configured directory"`
	Confirm string `json:"confirm" jsonschema:"required,exact release/namespace confirmation"`
}

type deployPlanOutput struct {
	Release     string   `json:"release"`
	Namespace   string   `json:"namespace"`
	KubeContext string   `json:"kubeContext"`
	Image       string   `json:"image"`
	Steps       []string `json:"steps"`
}

type deployPreflightOutput struct {
	Ready  bool          `json:"ready"`
	Report doctor.Report `json:"report"`
}

type doctorInput struct {
	Domain       string          `json:"domain" jsonschema:"required,DNS name to resolve on this Mac"`
	KubeContext  string          `json:"kubeContext" jsonschema:"required,explicit Kubernetes context"`
	RegistryURL  string          `json:"registryUrl" jsonschema:"optional loopback registry HTTP origin"`
	DiskPath     string          `json:"diskPath" jsonschema:"required,absolute build-volume path"`
	MinFreeBytes uint64          `json:"minFreeBytes,omitempty"`
	Services     []doctor.Target `json:"services,omitempty"`
}

func (input doctorInput) config() doctor.Config {
	return doctor.Config{Domain: input.Domain, KubeContext: input.KubeContext, RegistryURL: input.RegistryURL, DiskPath: input.DiskPath, MinFreeBytes: input.MinFreeBytes, Services: input.Services}
}

type doctorVerifyInput struct {
	Domain      string          `json:"domain" jsonschema:"required,DNS name to resolve"`
	KubeContext string          `json:"kubeContext" jsonschema:"required,explicit Kubernetes context"`
	RegistryURL string          `json:"registryUrl" jsonschema:"optional loopback registry origin"`
	DiskPath    string          `json:"diskPath" jsonschema:"required,absolute volume path"`
	Services    []doctor.Target `json:"services,omitempty"`
	CheckIDs    []string        `json:"checkIds" jsonschema:"required,check IDs to verify again"`
}

type doctorPlanOutput struct {
	Valid       bool `json:"valid"`
	MaxAttempts int  `json:"maxAttempts"`
	Steps       int  `json:"steps"`
}

type doctorRunner interface {
	Run(context.Context, doctor.Config) (doctor.Report, error)
}

var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}\.json$`)

func (cfg deployConfig) load(profile string) (deploy.Spec, error) {
	if cfg.ProfileDir == "" || !filepath.IsAbs(cfg.ProfileDir) {
		return deploy.Spec{}, errors.New("deployment_profiles_not_configured")
	}
	if !profileNamePattern.MatchString(profile) {
		return deploy.Spec{}, errors.New("invalid_deployment_profile_name")
	}
	base, err := filepath.EvalSymlinks(cfg.ProfileDir)
	if err != nil {
		return deploy.Spec{}, errors.New("deployment_profile_directory_unavailable")
	}
	file, err := filepath.EvalSymlinks(filepath.Join(base, profile))
	if err != nil {
		return deploy.Spec{}, errors.New("deployment_profile_unavailable")
	}
	rel, err := filepath.Rel(base, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return deploy.Spec{}, errors.New("deployment_profile_outside_directory")
	}
	return deploy.LoadSpec(file)
}

func (cfg deployConfig) profileAvailable() bool {
	if cfg.ProfileDir == "" || !filepath.IsAbs(cfg.ProfileDir) {
		return false
	}
	entries, err := os.ReadDir(cfg.ProfileDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if profileNamePattern.MatchString(entry.Name()) {
			if _, err := cfg.load(entry.Name()); err == nil {
				return true
			}
		}
	}
	return false
}

func newServerWithRunner(runner *async.Runner) *mcp.Server {
	return newServerWithServices(runner, deployConfig{ProfileDir: os.Getenv("KUCHDESK_DEPLOY_PROFILE_DIR"), ProjectRoot: os.Getenv("KUCHDESK_PROJECT_ROOT"), Enabled: os.Getenv("KUCHDESK_DEPLOY_ENABLED") == "true", Runner: deploy.ExecRunner{}}, doctor.Service{})
}

func newServerWithServices(runner *async.Runner, cfg deployConfig, diagnostics doctorRunner) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "kuchdesk", Version: releaseVersion}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_chart_values_catalog", Description: "List the canonical YAML values files used by KuchDesk charts.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string][]string, error) {
			return nil, chartvalues.Files, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_chart_values_read", Description: "Read canonical chart YAML; private and credential values are redacted.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, input valuesInput) (*mcp.CallToolResult, valuesOutput, error) {
			value, err := chartvalues.Inspect(cfg.ProjectRoot, input.Service, input.File)
			if err != nil {
				return nil, valuesOutput{}, err
			}
			return nil, valuesOutput{YAML: value}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_chart_values_set", Description: "Edit one existing key in a canonical chart YAML file. Returns no value or Secret content."},
		func(_ context.Context, _ *mcp.CallToolRequest, input valuesEditInput) (*mcp.CallToolResult, valuesEdited, error) {
			if err := chartvalues.Set(cfg.ProjectRoot, input.Service, input.File, input.Keys, input.ValueYAML); err != nil {
				return nil, valuesEdited{}, err
			}
			return nil, valuesEdited{Changed: true}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_capabilities", Description: "Show the local build identity and deployment configuration without reading secrets.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest, capabilityInput) (*mcp.CallToolResult, capabilityOutput, error) {
			return nil, capabilityOutput{Build: currentBuildIdentity(), Deployment: deploymentCapability{Enabled: cfg.Enabled, ProfileDirectoryConfigured: cfg.ProfileDir != "" && filepath.IsAbs(cfg.ProfileDir), ProfileAvailable: cfg.profileAvailable()}}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_service_access_status", Description: "Read catalog-constrained TCP/UDP publication status from this Mac. Listener, backend and connectivity evidence are separate; TCP checks do not verify application authentication and UDP requires request/response acceptance.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, serviceAccessOutput, error) {
			report, err := serviceaccess.Read(ctx, cfg.ProjectRoot)
			if err != nil {
				return nil, serviceAccessOutput{}, err
			}
			return nil, serviceAccessOutput{Report: report}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_deploy_plan", Description: "Read a configured local profile and return its deployment plan without running commands.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, input deployInput) (*mcp.CallToolResult, deployPlanOutput, error) {
			spec, err := cfg.load(input.Profile)
			if err != nil {
				return nil, deployPlanOutput{}, err
			}
			steps, err := spec.Plan()
			if err != nil {
				return nil, deployPlanOutput{}, err
			}
			return nil, deployPlanOutput{Release: spec.Release, Namespace: spec.Namespace, KubeContext: spec.KubeContext, Image: spec.ImageRepository + ":" + spec.ImageTag, Steps: steps}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_deploy_preflight", Description: "Run read-only host and cluster checks from a named deployment profile; report whether apply is ready.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, input deployInput) (*mcp.CallToolResult, deployPreflightOutput, error) {
			spec, err := cfg.load(input.Profile)
			if err != nil {
				return nil, deployPreflightOutput{}, err
			}
			report, checkErr := (deploy.Workflow{Preflight: cfg.Preflight}).Check(ctx, spec)
			return nil, deployPreflightOutput{Ready: checkErr == nil, Report: report}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_deploy_start", Description: "Start an opt-in local build, registry push, Helm upgrade, and rollout check from a named profile. Requires exact release/namespace confirmation."},
		func(_ context.Context, _ *mcp.CallToolRequest, input deployStartInput) (*mcp.CallToolResult, jobStarted, error) {
			if !cfg.Enabled {
				return nil, jobStarted{}, errors.New("deployment_not_enabled")
			}
			spec, err := cfg.load(input.Profile)
			if err != nil {
				return nil, jobStarted{}, err
			}
			if input.Confirm != spec.Release+"/"+spec.Namespace {
				return nil, jobStarted{}, errors.New("exact_release_namespace_confirmation_required")
			}
			if cfg.Runner == nil {
				return nil, jobStarted{}, errors.New("deployment_runner_unavailable")
			}
			key := spec.KubeContext + "/" + spec.Namespace + "/" + spec.Release
			id, err := runner.Submit(key, 20*time.Minute, func(ctx context.Context) (any, error) {
				result, err := (deploy.Workflow{Runner: cfg.Runner, Preflight: cfg.Preflight}).Apply(ctx, spec)
				if err != nil {
					return nil, err
				}
				return result, nil
			})
			if err != nil {
				return nil, jobStarted{}, publicError(err)
			}
			return nil, jobStarted{ID: id}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_job_status", Description: "Read a bounded in-process KuchDesk operation status.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, input jobInput) (*mcp.CallToolResult, jobOutput, error) {
			status, err := runner.Status(input.ID)
			if err != nil {
				return nil, jobOutput{}, publicError(err)
			}
			return nil, outputFor(status), nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_cancel_job", Description: "Request cancellation of a queued or running KuchDesk operation."},
		func(_ context.Context, _ *mcp.CallToolRequest, input jobInput) (*mcp.CallToolResult, jobOutput, error) {
			status, err := runner.Cancel(input.ID)
			if err != nil {
				return nil, jobOutput{}, publicError(err)
			}
			return nil, outputFor(status), nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_doctor", Description: "Run bounded read-only Mac, DNS, Colima containerd, K3s, optional registry, resource, and Deployment checks; use the MCP Doctor prompt for AI-assisted diagnosis.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, input doctorInput) (*mcp.CallToolResult, doctor.Report, error) {
			report, err := diagnostics.Run(ctx, input.config())
			if err != nil {
				return nil, doctor.Report{}, err
			}
			return nil, report, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_doctor_validate_plan", Description: "Validate a bounded typed repair plan without executing it. Validation does not grant permission.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, input doctor.RepairPlan) (*mcp.CallToolResult, doctorPlanOutput, error) {
			if err := doctor.ValidatePlan(input); err != nil {
				return nil, doctorPlanOutput{}, err
			}
			for _, step := range input.Steps {
				if step.Tool != "kuchdesk_deploy_start" {
					continue
				}
				if !cfg.Enabled {
					return nil, doctorPlanOutput{}, errors.New("deployment_not_enabled")
				}
				spec, err := cfg.load(step.Profile)
				if err != nil {
					return nil, doctorPlanOutput{}, err
				}
				if step.Confirm != spec.Release+"/"+spec.Namespace {
					return nil, doctorPlanOutput{}, errors.New("exact_release_namespace_confirmation_required")
				}
			}
			return nil, doctorPlanOutput{Valid: true, MaxAttempts: 2, Steps: len(input.Steps)}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_doctor_verify", Description: "Rerun read-only Doctor checks and verify affected check IDs after an action.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, input doctorVerifyInput) (*mcp.CallToolResult, doctor.Verification, error) {
			config := doctor.Config{Domain: input.Domain, KubeContext: input.KubeContext, RegistryURL: input.RegistryURL, DiskPath: input.DiskPath, Services: input.Services}
			report, err := diagnostics.Run(ctx, config)
			if err != nil {
				return nil, doctor.Verification{}, err
			}
			result, err := doctor.Verify(report, input.CheckIDs)
			return nil, result, err
		})
	server.AddPrompt(&mcp.Prompt{Name: "kuchdesk_doctor_repair", Title: "Diagnose and verify KuchDesk", Description: "Fresh Doctor report and bounded MCP-only repair workflow", Arguments: []*mcp.PromptArgument{
		{Name: "domain", Required: true}, {Name: "kubeContext", Required: true}, {Name: "registryUrl", Required: false}, {Name: "diskPath", Required: true}, {Name: "services", Required: false, Description: "Comma-separated namespace/deployment targets"},
	}}, func(ctx context.Context, request *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		if request == nil || request.Params == nil {
			return nil, errors.New("Doctor prompt arguments required")
		}
		config, err := doctorPromptConfig(request.Params.Arguments)
		if err != nil {
			return nil, err
		}
		report, err := diagnostics.Run(ctx, config)
		if err != nil {
			return nil, err
		}
		body, err := doctor.RenderPrompt(config, report)
		if err != nil {
			return nil, err
		}
		return &mcp.GetPromptResult{Description: "Current evidence and supported typed workflow", Messages: []*mcp.PromptMessage{{Role: mcp.Role("user"), Content: &mcp.TextContent{Text: body}}}}, nil
	})
	server.AddResource(&mcp.Resource{Name: "KuchDesk Doctor report schema", URI: "kuchdesk://doctor/report/v2", MIMEType: "application/schema+json"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "kuchdesk://doctor/report/v2", MIMEType: "application/schema+json", Text: doctor.ReportSchema()}}}, nil
	})
	server.AddResource(&mcp.Resource{Name: "KuchDesk Doctor prompt template", URI: "kuchdesk://doctor/prompt/v2", MIMEType: "text/markdown"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "kuchdesk://doctor/prompt/v2", MIMEType: "text/markdown", Text: doctor.PromptTemplate()}}}, nil
	})
	return server
}

func newServerWithScope(runner *async.Runner, cfg deployConfig, diagnostics doctorRunner, scope string) *mcp.Server {
	server := newServerWithServices(runner, cfg, diagnostics)
	switch scope {
	case "doctor-repair":
	case "doctor-readonly":
		server.RemoveTools("kuchdesk_deploy_plan", "kuchdesk_deploy_preflight", "kuchdesk_deploy_start", "kuchdesk_job_status", "kuchdesk_cancel_job", "kuchdesk_doctor_validate_plan", "kuchdesk_chart_values_set")
		server.RemovePrompts("kuchdesk_doctor_repair")
		server.RemoveResources("kuchdesk://doctor/prompt/v2")
	default:
		panic("unsupported MCP tool scope")
	}
	return server
}

func doctorPromptConfig(arguments map[string]string) (doctor.Config, error) {
	config := doctor.Config{Domain: arguments["domain"], KubeContext: arguments["kubeContext"], RegistryURL: arguments["registryUrl"], DiskPath: arguments["diskPath"]}
	if raw := strings.TrimSpace(arguments["services"]); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			pieces := strings.Split(strings.TrimSpace(part), "/")
			if len(pieces) != 2 {
				return doctor.Config{}, errors.New("invalid Doctor deployment target")
			}
			config.Services = append(config.Services, doctor.Target{Namespace: pieces[0], Deployment: pieces[1]})
		}
	}
	if err := config.Validate(); err != nil {
		return doctor.Config{}, err
	}
	return config, nil
}

func outputFor(status async.Snapshot) jobOutput {
	output := jobOutput{ID: status.ID, State: status.State, Error: status.Error}
	if deployment, ok := status.Result.(deploy.Result); ok {
		output.Deployment = &deployment
	}
	return output
}

func publicError(err error) error {
	switch {
	case errors.Is(err, async.ErrCapacity):
		return errors.New("operation_capacity_reached")
	case errors.Is(err, async.ErrNotFound):
		return errors.New("operation_not_found")
	default:
		return errors.New("operation_failed")
	}
}

func main() {
	scope := flag.String("scope", "doctor-repair", "MCP tool scope: doctor-repair or doctor-readonly")
	flag.Parse()
	if (*scope != "doctor-repair" && *scope != "doctor-readonly") || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "provide a valid -scope and no positional arguments")
		os.Exit(2)
	}
	runner := async.NewRunner(context.Background(), 4, 64)
	defer runner.Close()
	cfg := deployConfig{ProfileDir: os.Getenv("KUCHDESK_DEPLOY_PROFILE_DIR"), ProjectRoot: os.Getenv("KUCHDESK_PROJECT_ROOT"), Enabled: os.Getenv("KUCHDESK_DEPLOY_ENABLED") == "true", Runner: deploy.ExecRunner{}}
	server := newServerWithScope(runner, cfg, doctor.Service{}, *scope)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "KuchDesk MCP server stopped")
		os.Exit(1)
	}
}
