package grafanaauth_test

import (
	"os/exec"
	"strings"
	"testing"
)

func render(t *testing.T, template string, extra ...string) (string, error) {
	return renderWithProfile(t, "values.homelab.yaml", template, extra...)
}
func renderWithProfile(t *testing.T, profile, template string, extra ...string) (string, error) {
	t.Helper()
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is required for chart contract tests")
	}
	args := []string{"template", "grafana", "../../apps/observability/grafana", "-n", "observability", "-f", "../../apps/observability/grafana/" + profile}
	if template != "" {
		args = append(args, "--show-only", template)
	}
	out, err := exec.Command(helm, append(args, extra...)...).CombinedOutput()
	return string(out), err
}

func TestDirectValuesCreateGrafanaAdminSecret(t *testing.T) {
	out, err := render(t, "templates/secret.yaml", "--set-string", "adminUser=fixture_admin", "--set-string", "adminPassword=fixture_password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "kind: Secret") || !strings.Contains(out, "admin-user:") || !strings.Contains(out, "admin-password:") {
		t.Fatal("direct values did not create the admin Secret")
	}
	deployment, err := render(t, "templates/deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(deployment, "secrets.infisical.com") {
		t.Fatal("obsolete reload annotation remains")
	}
}

func TestDirectValuesCreateRendererTokenSecret(t *testing.T) {
	out, err := render(t, "templates/image-renderer-secret.yaml", "--set-string", "imageRenderer.token=fixture-renderer-token")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "kind: Secret") || !strings.Contains(out, "token:") {
		t.Fatal("direct values did not create the renderer Secret")
	}
}

func TestFreshHomelabRequiresAuthentication(t *testing.T) {
	config, err := render(t, "templates/configmap.yaml")
	if err != nil {
		t.Fatal(err, config)
	}
	var anonymous []string
	inside := false
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inside = line == "[auth.anonymous]"
			continue
		}
		if inside {
			anonymous = append(anonymous, line)
		}
	}
	section := strings.Join(anonymous, "\n")
	for _, want := range []string{"enabled = false"} {
		if !strings.Contains(section, want) {
			t.Fatalf("fresh homelab render loses authentication setting %q: %s", want, section)
		}
	}
	if strings.Contains(section, "org_role = Admin") {
		t.Fatal("anonymous server administration must remain excluded")
	}
}

func TestHomelabRoutesUnannotatedDashboardFilesIntoPlatform(t *testing.T) {
	for _, profile := range []string{"values.homelab.yaml", "values.development.yaml"} {
		deployment, err := renderWithProfile(t, profile, "templates/deployment.yaml")
		if err != nil {
			t.Fatal(err, deployment)
		}
		if !strings.Contains(deployment, "value: \"/tmp/dashboards/Platform\"") {
			t.Fatal("unannotated dashboard files still route to General")
		}
		provider, err := renderWithProfile(t, profile, "templates/configmap-dashboard-provider.yaml")
		if err != nil {
			t.Fatal(err, provider)
		}
		if !strings.Contains(provider, "foldersFromFilesStructure: true") || !strings.Contains(provider, "allowUiUpdates: false") {
			t.Fatal("folder creation or provisioned dashboard ownership changed")
		}
		if !strings.Contains(provider, "path: /tmp/dashboards\n") {
			t.Fatal("provider must scan the parent of the default Platform subfolder")
		}
	}
}

func TestProjectDashboardsKeepOnePlatformDirectory(t *testing.T) {
	out, err := exec.Command("kubectl", "kustomize", "../../apps/observability/grafana/manifests/dashboard").CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	if strings.Count(string(out), "grafana_folder: /tmp/dashboards/Platform") != 2 {
		t.Fatal("relative annotation would nest Platform twice after changing the default sidecar directory")
	}
}

func TestDevelopmentDashboardPollingNeedsNoAdminReload(t *testing.T) {
	for _, profile := range []string{"values.homelab.yaml", "values.development.yaml"} {
		t.Run(profile, func(t *testing.T) {
			deployment, err := renderWithProfile(t, profile, "templates/deployment.yaml")
			if err != nil {
				t.Fatal(err, deployment)
			}
			for _, forbidden := range []string{"name: REQ_URL", "name: REQ_USERNAME", "name: REQ_PASSWORD", "name: REQ_METHOD"} {
				if strings.Contains(deployment, forbidden) {
					t.Fatalf("dashboard polling still depends on admin reload: %s", forbidden)
				}
			}
			if !strings.Contains(deployment, "name: grafana-init-sc-dashboard") {
				t.Fatal("dashboard files must be collected before Grafana starts")
			}
			start := strings.Index(deployment, "- name: grafana-init-sc-dashboard")
			init := deployment[start:]
			end := strings.Index(init, "\n      containers:")
			if end < 0 {
				t.Fatal("dashboard watcher must be a native init sidecar")
			}
			init = init[:end]
			for _, required := range []string{"restartPolicy: Always", "startupProbe:", "path: /healthz", "port: 8084", "name: METHOD\n              value: WATCH"} {
				if !strings.Contains(init, required) {
					t.Fatalf("native watcher startup contract missing: %s", required)
				}
			}
			if strings.Contains(deployment, "- name: grafana-sc-dashboard") {
				t.Fatal("native dashboard watcher must not be duplicated as a regular container")
			}
			for _, preserved := range []string{"name: GF_SECURITY_ADMIN_USER", "name: GF_SECURITY_ADMIN_PASSWORD", "key: admin-user", "key: admin-password"} {
				if !strings.Contains(deployment, preserved) {
					t.Fatalf("Grafana bootstrap mapping changed: %s", preserved)
				}
			}
			provider, err := renderWithProfile(t, profile, "templates/configmap-dashboard-provider.yaml")
			if err != nil {
				t.Fatal(err, provider)
			}
			for _, required := range []string{"updateIntervalSeconds: 30", "path: /tmp/dashboards", "foldersFromFilesStructure: true", "allowUiUpdates: false"} {
				if !strings.Contains(provider, required) {
					t.Fatalf("automatic file polling contract missing: %s", required)
				}
			}
		})
	}
}
