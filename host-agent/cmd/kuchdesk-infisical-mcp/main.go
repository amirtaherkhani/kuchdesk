package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/async"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/deploy"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/infisical"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/mcpaccess"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type capabilityInput struct{}

type capability struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type capabilityOutput struct {
	BaseProject  platform.Profile     `json:"baseProject"`
	Configured   bool                 `json:"configured"`
	Capabilities []capability         `json:"capabilities"`
	Management   managementCapability `json:"management"`
	Deployment   deploymentCapability `json:"deployment"`
	TestCoverage string               `json:"testCoverage"`
}

type managementCapability struct {
	Exposed                bool     `json:"exposed"`
	PolicyMode             string   `json:"policyMode"`
	ProjectCreationEnabled bool     `json:"projectCreationEnabled"`
	Operations             []string `json:"operations,omitempty"`
	PermissionStatus       string   `json:"permissionStatus"`
}

type deploymentCapability struct {
	Enabled                    bool `json:"enabled"`
	ProfileDirectoryConfigured bool `json:"profileDirectoryConfigured"`
	ProfileAvailable           bool `json:"profileAvailable"`
}

type listInput struct {
	ProjectID   string `json:"projectId" jsonschema:"required,Infisical project ID"`
	Environment string `json:"environment" jsonschema:"required,Infisical environment slug"`
	SecretPath  string `json:"secretPath" jsonschema:"required,absolute secret path"`
}

type listOutput struct {
	Secrets []infisical.SecretName `json:"secrets"`
}

type jobInput struct {
	ID string `json:"id" jsonschema:"required,operation ID"`
}

type jobStarted struct {
	ID string `json:"id"`
}

type jobOutput struct {
	ID         string                 `json:"id"`
	State      async.State            `json:"state"`
	Secrets    []infisical.SecretName `json:"secrets,omitempty"`
	Deployment *deploy.Result         `json:"deployment,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

func newServer(client *infisical.Client) *mcp.Server {
	return newServerWithRunner(infisical.NewService(client), async.NewRunner(context.Background(), 4, 64))
}

func newServerWithRunner(service *infisical.Service, runner *async.Runner) *mcp.Server {
	return newServerWithDeploy(service, runner, deployConfig{ProfileDir: os.Getenv("KUCHDESK_DEPLOY_PROFILE_DIR"), Enabled: os.Getenv("KUCHDESK_DEPLOY_ENABLED") == "true", Runner: deploy.ExecRunner{}})
}

type deployConfig struct {
	ProfileDir string
	Enabled    bool
	Runner     deploy.Runner
	Preflight  deploy.Preflight
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
	RegistryURL  string          `json:"registryUrl" jsonschema:"required,loopback registry HTTP origin"`
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
	RegistryURL string          `json:"registryUrl" jsonschema:"required,loopback registry origin"`
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

func newServerWithDeploy(service *infisical.Service, runner *async.Runner, cfg deployConfig) *mcp.Server {
	return newServerWithServices(service, runner, cfg, doctor.Service{})
}

func newServerWithServices(service *infisical.Service, runner *async.Runner, cfg deployConfig, diagnostics doctorRunner) *mcp.Server {
	return newServerWithManagement(service, runner, cfg, diagnostics, nil)
}

func newServerWithManagement(service *infisical.Service, runner *async.Runner, cfg deployConfig, diagnostics doctorRunner, commands *infisical.CommandService) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "kuchdesk-infisical", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "infisical_capabilities", Description: "Show implemented Infisical tools and local credential configuration without accessing secrets.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(context.Context, *mcp.CallToolRequest, capabilityInput) (*mcp.CallToolResult, capabilityOutput, error) {
			profile, err := platform.Load(os.Getenv)
			if err != nil {
				return nil, capabilityOutput{}, err
			}
			management := managementCapability{Exposed: commands != nil, PolicyMode: "none", PermissionStatus: "not_checked"}
			if commands != nil {
				management.PolicyMode = "static_allowlist"
				if commands.MCPPolicy != nil {
					management.PolicyMode = "dynamic"
				}
				management.ProjectCreationEnabled = commands.AllowProjectCreate && commands.MCPPolicy == nil
				management.Operations = infisical.SupportedOperations()
			}
			deployment := deploymentCapability{Enabled: cfg.Enabled, ProfileDirectoryConfigured: cfg.ProfileDir != "" && filepath.IsAbs(cfg.ProfileDir), ProfileAvailable: cfg.profileAvailable()}
			manageStatus := "implemented; tool not exposed in this process"
			if management.Exposed {
				manageStatus = "implemented; tool exposed; permission checked per request"
			}
			deployStatus := "implemented; start disabled"
			if deployment.Enabled {
				deployStatus = "implemented; start enabled; profile and preflight required"
			}
			return nil, capabilityOutput{BaseProject: profile, Configured: service.Configured(), Management: management, Deployment: deployment, TestCoverage: "not_asserted_by_discovery", Capabilities: []capability{
				{Name: "list_secret_names", Status: "implemented; permission checked per request"},
				{Name: "async_name_listing", Status: "implemented in bounded process memory"},
				{Name: "secret_values", Status: "not returned by ordinary MCP tools"},
				{Name: "secret_writes", Status: manageStatus},
				{Name: "project_admin", Status: manageStatus},
				{Name: "helm_deploy", Status: deployStatus},
			}}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "infisical_list_secret_names", Description: "List only secret names and paths in one explicit project, environment, and path. Values are never returned.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(ctx context.Context, _ *mcp.CallToolRequest, input listInput) (*mcp.CallToolResult, listOutput, error) {
			secrets, err := service.ListSecretNames(ctx, infisical.Scope{ProjectID: input.ProjectID, Environment: input.Environment, SecretPath: input.SecretPath})
			if err != nil {
				return nil, listOutput{}, publicError(err)
			}
			return nil, listOutput{Secrets: secrets}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "infisical_start_list_secret_names", Description: "Start a bounded in-process name-only secret listing operation.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, input listInput) (*mcp.CallToolResult, jobStarted, error) {
			scope := infisical.Scope{ProjectID: input.ProjectID, Environment: input.Environment, SecretPath: input.SecretPath}
			if err := infisical.ValidateScope(scope); err != nil {
				return nil, jobStarted{}, publicError(err)
			}
			id, err := runner.Submit("", 30*time.Second, func(ctx context.Context) (any, error) {
				secrets, err := service.ListSecretNames(ctx, scope)
				if err != nil {
					return nil, publicError(err)
				}
				return secrets, nil
			})
			if err != nil {
				return nil, jobStarted{}, publicError(err)
			}
			return nil, jobStarted{ID: id}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "infisical_job_status", Description: "Read the status of an in-process operation. Completed results are retained only in bounded memory.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
		func(_ context.Context, _ *mcp.CallToolRequest, input jobInput) (*mcp.CallToolResult, jobOutput, error) {
			status, err := runner.Status(input.ID)
			if err != nil {
				return nil, jobOutput{}, publicError(err)
			}
			return nil, outputFor(status), nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "infisical_cancel_job", Description: "Request cancellation of a queued or running in-process operation."},
		func(_ context.Context, _ *mcp.CallToolRequest, input jobInput) (*mcp.CallToolResult, jobOutput, error) {
			status, err := runner.Cancel(input.ID)
			if err != nil {
				return nil, jobOutput{}, publicError(err)
			}
			return nil, outputFor(status), nil
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
	mcp.AddTool(server, &mcp.Tool{Name: "kuchdesk_doctor", Description: "Run bounded read-only Mac, DNS, Docker, KIND, registry, resource, and Deployment checks; use the MCP Doctor prompt for AI-assisted diagnosis.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}},
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
		{Name: "domain", Required: true}, {Name: "kubeContext", Required: true}, {Name: "registryUrl", Required: true}, {Name: "diskPath", Required: true}, {Name: "services", Required: false, Description: "Comma-separated namespace/deployment targets"},
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
	if commands != nil {
		registerInfisicalManagement(server, *commands)
	}
	return server
}

// Scope is applied before an MCP client connects. In Doctor modes the
// Infisical tools are removed, so no credential backend is needed.
func newServerWithScope(service *infisical.Service, runner *async.Runner, cfg deployConfig, diagnostics doctorRunner, scope string) *mcp.Server {
	return newServerWithScopeAndManagement(service, runner, cfg, diagnostics, scope, nil)
}

func newServerWithScopeAndManagement(service *infisical.Service, runner *async.Runner, cfg deployConfig, diagnostics doctorRunner, scope string, commands *infisical.CommandService) *mcp.Server {
	server := newServerWithManagement(service, runner, cfg, diagnostics, commands)
	switch scope {
	case "all":
	case "doctor-repair", "doctor-readonly":
		server.RemoveTools("infisical_capabilities", "infisical_list_secret_names", "infisical_start_list_secret_names", "infisical_job_status", "infisical_cancel_job", "infisical_manage")
		if scope == "doctor-readonly" {
			server.RemoveTools("kuchdesk_deploy_plan", "kuchdesk_deploy_preflight", "kuchdesk_deploy_start", "kuchdesk_job_status", "kuchdesk_cancel_job", "kuchdesk_doctor_validate_plan")
			server.RemovePrompts("kuchdesk_doctor_repair")
			server.RemoveResources("kuchdesk://doctor/prompt/v2")
		}
	default:
		panic("unsupported MCP tool scope")
	}
	return server
}

func registerInfisicalManagement(server *mcp.Server, commands infisical.CommandService) {
	mcp.AddTool(server, &mcp.Tool{Name: "infisical_manage", Description: "Execute one explicitly scoped Infisical project, environment, folder, secret, role, or identity-membership command. Writes require operation:target confirmation. Responses never include secret values."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input infisical.Command) (*mcp.CallToolResult, infisical.CommandResult, error) {
			result, err := commands.Execute(ctx, input)
			if err != nil {
				return nil, infisical.CommandResult{}, infisical.PublicError(err)
			}
			return nil, result, nil
		})
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
	if names, ok := status.Result.([]infisical.SecretName); ok {
		output.Secrets = names
	}
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
		return infisical.PublicError(err)
	}
}

func main() {
	baseURL := flag.String("url", "", "Infisical HTTPS origin (required only with -scope all)")
	scope := flag.String("scope", "all", "MCP tool scope: all, doctor-repair, or doctor-readonly")
	manage := flag.Bool("manage", false, "expose scoped Infisical management commands in all scope")
	projects := flag.String("project-ids", "", "comma-separated Infisical project ID allowlist for management")
	allowProjectCreate := flag.Bool("allow-project-create", false, "allow organization-level project creation")
	projectPolicy := flag.String("project-policy", "", "path to the existing private Host enrollment policy (opt-in dynamic MCP policy)")
	clientContext := flag.String("client-context", "", "trusted MCP client context key configured in the Host policy")
	flag.Parse()
	if (*scope != "all" && *scope != "doctor-repair" && *scope != "doctor-readonly") || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "provide a valid -scope and no positional arguments")
		os.Exit(2)
	}
	if *manage && *scope != "all" {
		fmt.Fprintln(os.Stderr, "Infisical management requires -scope all")
		os.Exit(2)
	}
	if (*projectPolicy == "") != (*clientContext == "") || ((*projectPolicy != "" || *clientContext != "") && !*manage) {
		fmt.Fprintln(os.Stderr, "-project-policy and -client-context must be used together with -manage")
		os.Exit(2)
	}
	if *projectPolicy != "" && *allowProjectCreate {
		fmt.Fprintln(os.Stderr, "-allow-project-create is unavailable with dynamic project policy")
		os.Exit(2)
	}
	var service *infisical.Service
	var client *infisical.Client
	if *scope == "all" {
		if *baseURL == "" {
			fmt.Fprintln(os.Stderr, "provide -url with the Infisical HTTPS origin")
			os.Exit(2)
		}
		var err error
		client, err = hostClient(*baseURL, os.Getenv)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid Infisical configuration:", err)
			os.Exit(2)
		}
		service = infisical.NewService(client)
	} else if *baseURL != "" {
		fmt.Fprintln(os.Stderr, "-url is only valid with -scope all")
		os.Exit(2)
	}
	runner := async.NewRunner(context.Background(), 4, 64)
	defer runner.Close()
	cfg := deployConfig{ProfileDir: os.Getenv("KUCHDESK_DEPLOY_PROFILE_DIR"), Enabled: os.Getenv("KUCHDESK_DEPLOY_ENABLED") == "true", Runner: deploy.ExecRunner{}}
	var commands *infisical.CommandService
	if *manage {
		allowed := make(map[string]bool)
		for _, raw := range strings.Split(*projects, ",") {
			if id := strings.TrimSpace(raw); id != "" {
				allowed[id] = true
			}
		}
		if len(allowed) == 0 && !*allowProjectCreate && *projectPolicy == "" {
			fmt.Fprintln(os.Stderr, "Infisical management requires a project allowlist")
			os.Exit(2)
		}
		configured := infisical.CommandService{Client: client, AllowedProjects: allowed, AllowProjectCreate: *allowProjectCreate}
		if *projectPolicy != "" {
			authorizer, err := mcpaccess.NewAuthorizer(*projectPolicy, *clientContext, client)
			if err != nil {
				fmt.Fprintln(os.Stderr, "invalid dynamic MCP project policy:", err)
				os.Exit(2)
			}
			configured.MCPPolicy = authorizer
		}
		commands = &configured
	}
	server := newServerWithScopeAndManagement(service, runner, cfg, doctor.Service{}, *scope, commands)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "Infisical MCP server stopped")
		os.Exit(1)
	}
}

func hostClient(baseURL string, getenv func(string) string) (*infisical.Client, error) {
	clientID, clientSecret, err := infisical.HostCredentials(getenv)
	if err != nil {
		return nil, err
	}
	return infisical.NewClient(baseURL, clientID, clientSecret, nil)
}
