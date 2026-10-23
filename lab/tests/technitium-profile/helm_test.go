package technitiumprofile_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestFreshTechnitiumUsesDirectPasswordAndClaim(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is required for chart contract tests")
	}
	chart := "../../apps/platform/technitium"
	out, err := exec.Command(helm, "template", "technitium", chart, "-n", "technitium", "--set-string", "adminPassword=fixture-only").Output()
	if err != nil {
		t.Fatal("Technitium chart did not render")
	}
	rendered := string(out)
	for _, want := range []string{
		"name: technitium-admin", "DNS_SERVER_ADMIN_PASSWORD:", "name: technitium-lan",
		"storageClassName: \"local-path\"", "mountPath: /etc/dns",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("fresh chart is missing %q", want)
		}
	}
	if strings.Contains(rendered, "existingClaim:") {
		t.Fatal("fresh chart must create its own claim")
	}
	if _, err := exec.Command(helm, "template", "technitium", chart, "-n", "technitium").Output(); err == nil {
		t.Fatal("chart accepted a missing first-start password")
	}
}
