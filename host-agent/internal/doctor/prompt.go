package doctor

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

//go:embed prompt.md
var promptTemplate string

//go:embed doctor-report.schema.json
var reportSchema string

type ToolParameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type ToolCapability struct {
	Name          string          `json:"name"`
	Parameters    []ToolParameter `json:"parameters"`
	Preconditions []string        `json:"preconditions"`
	Impact        string          `json:"impact"`
	DryRun        string          `json:"dryRun"`
	Rollback      string          `json:"rollback"`
	PostCheck     string          `json:"postCheck"`
	Mutating      bool            `json:"mutating"`
}

func ToolCatalog() []ToolCapability {
	profile := ToolParameter{"profile", "string", true, "Trusted .json filename under configured deploy profile directory"}
	return []ToolCapability{
		{"kuchdesk_doctor", []ToolParameter{{"domain", "string", true, "Host DNS name"}, {"kubeContext", "string", true, "Explicit Kubernetes context"}, {"registryUrl", "string", false, "Loopback HTTP origin"}, {"diskPath", "string", true, "Absolute build-volume path"}, {"services", "array", false, "Up to 16 namespace/deployment targets"}}, []string{"Valid local scope"}, "Read-only diagnostic snapshot", "Always read-only", "Not applicable", "Use the new report as baseline", false},
		{"kuchdesk_deploy_plan", []ToolParameter{profile}, []string{"Profile directory configured"}, "Read-only deployment plan", "Always read-only", "Not applicable", "Compare profile and affected check", false},
		{"kuchdesk_deploy_preflight", []ToolParameter{profile}, []string{"Profile directory configured"}, "Read-only host and cluster readiness checks", "Always read-only", "Not applicable", "All required preflight checks must be OK", false},
		{"kuchdesk_doctor_validate_plan", []ToolParameter{{"steps", "array", true, "At most two plan/preflight/start/verify sequences"}}, []string{"Use cataloged tool names only"}, "Validates typed plan without execution", "Always read-only", "Not applicable", "A valid plan is not authorization", false},
		{"kuchdesk_deploy_start", []ToolParameter{profile, {"confirm", "string", true, "Exact release/namespace from profile"}}, []string{"Deployment opt-in enabled", "Matching plan and successful preflight", "Action-time authorization and exact confirmation"}, "Build Colima containerd image and Helm 4 upgrade", "Call deploy plan and preflight first; no apply dry-run", "Helm rollback-on-failure covers Helm failure only; later rollout failure needs inspection", "Wait for job then rerun Doctor verification", true},
		{"kuchdesk_job_status", []ToolParameter{{"id", "string", true, "Job ID from deployment start"}}, []string{"Known in-process job ID"}, "Read-only job state", "Always read-only", "Not applicable", "Success means workflow completed, then verify health", false},
		{"kuchdesk_doctor_verify", []ToolParameter{{"domain", "string", true, "Same DNS name"}, {"kubeContext", "string", true, "Same context"}, {"registryUrl", "string", false, "Same registry"}, {"diskPath", "string", true, "Same volume"}, {"checkIds", "array", true, "Affected check IDs from baseline"}}, []string{"Fresh diagnostic scope matches baseline"}, "Read-only post-action check", "Always read-only", "Not applicable", "Only fresh OK checks count as repaired", false},
	}
}

func PromptTemplate() string { return promptTemplate }
func ReportSchema() string   { return reportSchema }

func RenderPrompt(config Config, report Report) (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}
	if report.SchemaVersion != "kuchdesk.doctor/v2" {
		return "", errors.New("Doctor report schema version mismatch")
	}
	report.Checks = append([]Check(nil), report.Checks...)
	for index := range report.Checks {
		if report.Checks[index].RawExcerpt == "" {
			continue
		}
		report.Checks[index].RawExcerpt, report.Checks[index].Truncated = NormalizeExcerpt([]byte(report.Checks[index].RawExcerpt))
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		return "", err
	}
	catalogJSON, err := json.Marshal(ToolCatalog())
	if err != nil {
		return "", err
	}
	return strings.NewReplacer("{{CONFIG_JSON}}", string(configJSON), "{{REPORT_JSON}}", string(reportJSON), "{{CATALOG_JSON}}", string(catalogJSON)).Replace(promptTemplate), nil
}

type PlanStep struct {
	Tool     string   `json:"tool"`
	Profile  string   `json:"profile,omitempty"`
	Confirm  string   `json:"confirm,omitempty"`
	CheckIDs []string `json:"checkIds,omitempty"`
}

type RepairPlan struct {
	Steps []PlanStep `json:"steps"`
}

func (p *RepairPlan) UnmarshalJSON(data []byte) error {
	type plain RepairPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value plain
	if err := decoder.Decode(&value); err != nil {
		return errors.New("invalid Doctor repair plan")
	}
	if !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return errors.New("invalid Doctor repair plan")
	}
	*p = RepairPlan(value)
	return nil
}

// ValidatePlan rejects invented tools and limits repair attempts. This is a
// shape check only; current permissions and approval still govern execution.
func ValidatePlan(plan RepairPlan) error {
	if len(plan.Steps) > 8 {
		return errors.New("Doctor plan exceeds eight steps")
	}
	if len(plan.Steps) == 0 {
		return nil
	}
	state, attempts, profile := 0, 0, ""
	for _, step := range plan.Steps {
		switch state {
		case 0:
			if step.Tool != "kuchdesk_deploy_plan" || step.Profile == "" || step.Confirm != "" || len(step.CheckIDs) != 0 {
				return errors.New("Doctor plan must start with a typed deployment plan")
			}
			if profile != "" && profile != step.Profile {
				return errors.New("Doctor plan cannot mix deployment profiles")
			}
			profile, state = step.Profile, 1
		case 1:
			if step.Tool != "kuchdesk_deploy_preflight" || step.Profile != profile || step.Confirm != "" || len(step.CheckIDs) != 0 {
				return errors.New("Doctor plan requires matching preflight")
			}
			state = 2
		case 2:
			if step.Tool != "kuchdesk_deploy_start" || step.Profile != profile || step.Confirm == "" || len(step.CheckIDs) != 0 {
				return errors.New("Doctor plan requires typed deployment start and confirmation")
			}
			attempts++
			if attempts > 2 {
				return errors.New("Doctor plan exceeds two repair attempts")
			}
			state = 3
		case 3:
			if step.Tool != "kuchdesk_doctor_verify" || step.Profile != "" || step.Confirm != "" || len(step.CheckIDs) == 0 || len(step.CheckIDs) > 16 {
				return errors.New("Doctor plan requires fresh verification after each action")
			}
			state = 0
		}
	}
	if state != 0 {
		return fmt.Errorf("Doctor plan ends before verification")
	}
	return nil
}

type Verification struct {
	Success  bool     `json:"success"`
	CheckIDs []string `json:"checkIds"`
	Report   Report   `json:"report"`
}

func Verify(report Report, ids []string) (Verification, error) {
	if len(ids) == 0 || len(ids) > 16 {
		return Verification{}, errors.New("one to sixteen check IDs are required")
	}
	known := make(map[string]string, len(report.Checks))
	for _, check := range report.Checks {
		known[check.ID] = check.Status
	}
	seen := make(map[string]bool, len(ids))
	verified := true
	for _, id := range ids {
		status, exists := known[id]
		if !exists || seen[id] {
			return Verification{}, errors.New("verification check ID missing or duplicated")
		}
		seen[id] = true
		if status != "ok" {
			verified = false
		}
	}
	return Verification{Success: verified, CheckIDs: ids, Report: report}, nil
}
