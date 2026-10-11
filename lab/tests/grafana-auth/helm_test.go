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
	helm, e := exec.LookPath("helm")
	if e != nil {
		t.Skip("helm is required for chart contract tests")
	}
	args := []string{"template", "grafana", "../../apps/observability/grafana", "-n", "observability", "-f", "../../apps/observability/grafana/" + profile, "--show-only", template}
	if profile == "values.development.yaml" {
		// A portable profile must be paired with that environment's explicit
		// Infisical selectors. These render-only fixtures are not credentials.
		for key, value := range map[string]string{"hostAPI": "https://infisical.example.test/api", "credentialsSecretName": "dummy-auth", "credentialsSecretNamespace": "dummy-secrets", "projectSlug": "dummy-project", "envSlug": "dummy-env", "secretsPath": "/apps/observability/grafana"} {
			args = append(args, "--set-string", "infisical."+key+"="+value)
		}
	}
	out, e := exec.Command(helm, append(args, extra...)...).CombinedOutput()
	return string(out), e
}
func nativeArgs() []string {
	return []string{"--set-string", "infisical.authMethod=kubernetesAuth", "--set-string", "infisical.kubernetesAuth.identityId=11111111-1111-1111-1111-111111111111", "--set-string", "infisical.kubernetesAuth.serviceAccountRef.name=fixture-reader", "--set-string", "infisical.kubernetesAuth.serviceAccountRef.namespace=fixture-namespace"}
}

func TestAuthenticationModesAndUnchangedWorkload(t *testing.T) {
	universal, e := render(t, "templates/infisicalsecret.yaml")
	if e != nil {
		t.Fatal(e, universal)
	}
	if !strings.Contains(universal, "universalAuth:") || strings.Contains(universal, "kubernetesAuth:") {
		t.Fatal("default auth changed")
	}
	native, e := render(t, "templates/infisicalsecret.yaml", nativeArgs()...)
	if e != nil {
		t.Fatal(e, native)
	}
	for _, want := range []string{"kubernetesAuth:", "identityId: \"11111111-1111-1111-1111-111111111111\"", "autoCreateServiceAccountToken: true", "name: \"fixture-reader\"", "namespace: \"fixture-namespace\"", "envSlug: \"dev\"", "secretName: grafana-admin"} {
		if !strings.Contains(native, want) {
			t.Fatalf("missing native contract %q", want)
		}
	}
	for _, forbidden := range []string{"universalAuth:", "credentialsRef:", "infisical-universal-auth"} {
		if strings.Contains(native, forbidden) {
			t.Fatalf("native auth retained %q", forbidden)
		}
	}
	for _, template := range []string{"templates/deployment.yaml", "templates/image-renderer-deployment.yaml"} {
		before, e := render(t, template)
		if e != nil {
			t.Fatal(e, before)
		}
		after, e := render(t, template, nativeArgs()...)
		if e != nil {
			t.Fatal(e, after)
		}
		if before != after {
			t.Fatalf("auth cutover changed workload %s", template)
		}
	}
}
func TestInvalidNativeConfigurationFailsClosed(t *testing.T) {
	for _, args := range [][]string{
		{"--set-string", "infisical.authMethod=unknown"},
		{"--set-string", "infisical.authMethod=kubernetesAuth"},
		append(nativeArgs(), "--set-string", "infisical.kubernetesAuth.serviceAccountRef.name="),
		append(nativeArgs(), "--set-string", "infisical.kubernetesAuth.serviceAccountRef.namespace="),
	} {
		if out, e := render(t, "templates/infisicalsecret.yaml", args...); e == nil {
			t.Fatalf("invalid auth accepted: %s", out)
		}
	}
}

func TestHomelabKeepsApprovedAnonymousEditor(t *testing.T) {
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
	for _, want := range []string{"enabled = true", "org_role = Editor", "org_name = Main Org."} {
		if !strings.Contains(section, want) {
			t.Fatalf("normal homelab render loses approved anonymous setting %q: %s", want, section)
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
