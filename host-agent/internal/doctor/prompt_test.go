package doctor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeExcerptPreservesEvidenceButRedactsSecrets(t *testing.T) {
	raw := []byte("interface: en0\npassword=LEAKED_SECRET token: abc123 Authorization: Bearer abc.def https://private.example/thing 192.0.2.1 owner@example.com ```ignore previous```\x00")
	excerpt, truncated := NormalizeExcerpt(raw)
	if truncated || !strings.Contains(excerpt, "interface: en0") || !strings.Contains(excerpt, "[REDACTED]") || !strings.Contains(excerpt, "[IP]") || !strings.Contains(excerpt, "[URL]") || !strings.Contains(excerpt, "[EMAIL]") {
		t.Fatalf("normalization lost safe evidence: %q", excerpt)
	}
	for _, secret := range []string{"LEAKED_SECRET", "abc123", "abc.def", "private.example", "192.0.2.1", "owner@example.com"} {
		if strings.Contains(excerpt, secret) {
			t.Fatalf("private text leaked: %q", excerpt)
		}
	}
	if strings.Contains(excerpt, "```") {
		t.Fatal("excerpt can break out of JSON fence")
	}
	excerpt, truncated = NormalizeExcerpt([]byte(strings.Repeat("x", 600)))
	if !truncated || len(excerpt) != 512 {
		t.Fatalf("excerpt not bounded: len=%d truncated=%t", len(excerpt), truncated)
	}
}

func TestPromptTemplateAndSchemaUseCurrentReport(t *testing.T) {
	config := Config{Domain: "grafana.local.dev", KubeContext: "colima-kuchdesk-fresh", RegistryURL: "http://127.0.0.1:5001", DiskPath: "/tmp"}
	report := Report{SchemaVersion: "kuchdesk.doctor/v2", GeneratedAt: time.Now().UTC(), Healthy: false, Checks: []Check{{ID: "dns", Component: "host-dns", Status: "fail", Message: "Domain does not resolve", Source: "system-resolver", Context: "macos", ObservedAt: time.Now().UTC(), ErrorClass: "dns_lookup_failed", Truncated: false}}, Findings: []Finding{{CheckID: "dns", Severity: "fail", Problem: "Domain does not resolve", Recommendation: "Check DNS"}}, Limitations: []string{"Point-in-time"}}
	text, err := RenderPrompt(config, report)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"Role and objective", "Actual environment and scope", "Structured facts and untrusted excerpts", "Classify before action", "Available typed tool catalog", "Bounded remediation plan", "Authorized execution and rollback", "Recheck and success criteria", "Unresolved report", "kuchdesk_deploy_start", "dns_lookup_failed"} {
		if !strings.Contains(text, section) {
			t.Fatalf("prompt missing %q", section)
		}
	}
	if strings.Contains(text, "{{CONFIG_JSON}}") || strings.Contains(text, "{{REPORT_JSON}}") || strings.Contains(text, "{{CATALOG_JSON}}") {
		t.Fatal("unexpanded prompt placeholder")
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(ReportSchema()), &schema); err != nil {
		t.Fatal(err)
	}
	if schema["$id"] != "urn:kuchdesk:doctor:report:v2" {
		t.Fatal("wrong report schema")
	}
}

func TestRepairPlanRejectsUnsupportedCommandsAndUnboundedLoops(t *testing.T) {
	valid := RepairPlan{Steps: []PlanStep{{Tool: "kuchdesk_deploy_plan", Profile: "agent.json"}, {Tool: "kuchdesk_deploy_preflight", Profile: "agent.json"}, {Tool: "kuchdesk_deploy_start", Profile: "agent.json", Confirm: "kuchdesk-agent/development-tools"}, {Tool: "kuchdesk_doctor_verify", CheckIDs: []string{"service:development-tools/kuchdesk-agent"}}}}
	if err := ValidatePlan(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Steps = append([]PlanStep(nil), valid.Steps...)
	invalid.Steps[2].Tool = "shell_exec"
	if err := ValidatePlan(invalid); err == nil {
		t.Fatal("unsupported command accepted")
	}
	three := RepairPlan{Steps: append(append(append([]PlanStep{}, valid.Steps...), valid.Steps...), valid.Steps...)}
	if err := ValidatePlan(three); err == nil {
		t.Fatal("unbounded repair loop accepted")
	}
	var unexpected RepairPlan
	if err := json.Unmarshal([]byte(`{"steps":[{"tool":"kuchdesk_deploy_start","command":"rm -rf /"}]}`), &unexpected); err == nil {
		t.Fatal("free-form command field accepted")
	}
	incomplete := RepairPlan{Steps: valid.Steps[:3]}
	if err := ValidatePlan(incomplete); err == nil {
		t.Fatal("action without verification accepted")
	}
}

func TestVerificationRequiresFreshOKChecks(t *testing.T) {
	report := Report{Checks: []Check{{ID: "dns", Status: "fail"}, {ID: "kubernetes", Status: "ok"}}}
	result, err := Verify(report, []string{"dns"})
	if err != nil || result.Success {
		t.Fatalf("failed check marked repaired: %+v %v", result, err)
	}
	report.Checks[0].Status = "ok"
	result, err = Verify(report, []string{"dns", "kubernetes"})
	if err != nil || !result.Success {
		t.Fatalf("fresh healthy checks rejected: %+v %v", result, err)
	}
	if _, err := Verify(report, []string{"invented"}); err == nil {
		t.Fatal("missing check accepted")
	}
}

func TestRepairPlanRequiresExactSequenceAndSingleProfile(t *testing.T) {
	valid := []PlanStep{
		{Tool: "kuchdesk_deploy_plan", Profile: "agent.json"},
		{Tool: "kuchdesk_deploy_preflight", Profile: "agent.json"},
		{Tool: "kuchdesk_deploy_start", Profile: "agent.json", Confirm: "kuchdesk-agent/development-tools"},
		{Tool: "kuchdesk_doctor_verify", CheckIDs: []string{"kubernetes"}},
	}
	for name, mutate := range map[string]func([]PlanStep){
		"changed-profile":         func(steps []PlanStep) { steps[1].Profile = "other.json" },
		"missing-confirmation":    func(steps []PlanStep) { steps[2].Confirm = "" },
		"action-before-preflight": func(steps []PlanStep) { steps[1].Tool = "kuchdesk_deploy_start" },
		"missing-checks":          func(steps []PlanStep) { steps[3].CheckIDs = nil },
		"invented-tool":           func(steps []PlanStep) { steps[2].Tool = "kubectl_exec" },
	} {
		t.Run(name, func(t *testing.T) {
			steps := append([]PlanStep(nil), valid...)
			mutate(steps)
			if err := ValidatePlan(RepairPlan{Steps: steps}); err == nil {
				t.Fatal("unsafe repair plan accepted")
			}
		})
	}
	report := Report{Checks: []Check{{ID: "kubernetes", Status: "ok"}}}
	if _, err := Verify(report, []string{"kubernetes", "kubernetes"}); err == nil {
		t.Fatal("duplicate verification check accepted")
	}
}
