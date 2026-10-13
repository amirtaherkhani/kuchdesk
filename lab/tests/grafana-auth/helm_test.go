package grafanaauth_test

import (
	"os/exec"
	"sort"
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
	args := []string{"template", "grafana", "../../apps/observability/grafana", "-n", "observability", "-f", "../../apps/observability/grafana/" + profile}
	if template != "" {
		args = append(args, "--show-only", template)
	}
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
	universal, e := render(t, "templates/infisicalsecret.yaml", "--set-string", "infisical.authMethod=universalAuth")
	if e != nil {
		t.Fatal(e, universal)
	}
	if !strings.Contains(universal, "universalAuth:") || strings.Contains(universal, "kubernetesAuth:") {
		t.Fatal("explicit Universal Auth option changed")
	}
	native, e := render(t, "templates/infisicalsecret.yaml", nativeArgs()...)
	if e != nil {
		t.Fatal(e, native)
	}
	for _, want := range []string{"kind: InfisicalConnection", "kind: InfisicalAuth", "kind: InfisicalStaticSecret", "kind: Secret", "identityId: \"11111111-1111-1111-1111-111111111111\"", "method: kubernetes", "name: \"fixture-reader\"", "namespace: \"fixture-namespace\"", "environmentSlug: \"dev\"", "name: grafana-admin", "creationPolicy: Orphan"} {
		if !strings.Contains(native, want) {
			t.Fatalf("missing native contract %q", want)
		}
	}
	for _, forbidden := range []string{"universalAuth:", "credentialsRef:", "infisical-universal-auth", "kind: InfisicalSecret\n", "creationPolicy: Owner", "autoCreateServiceAccountToken:"} {
		if strings.Contains(native, forbidden) {
			t.Fatalf("native auth retained %q", forbidden)
		}
	}
	for _, template := range []string{"templates/deployment.yaml", "templates/image-renderer-deployment.yaml"} {
		before, e := render(t, template, "--set-string", "infisical.authMethod=universalAuth")
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
		{"--set-string", "infisical.authMethod=kubernetesAuth", "--set-string", "infisical.kubernetesAuth.identityId="},
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
			for _, preserved := range []string{"name: GF_SECURITY_ADMIN_USER", "name: GF_SECURITY_ADMIN_PASSWORD", "key: GRAFANA_ADMIN_USER", "key: GRAFANA_ADMIN_PASSWORD"} {
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

// Only the secret synchronization resources may change during auth migration.
func TestNativeMigrationPreservesAllOtherChartResources(t *testing.T) {
	fixture := []string{"--set-string", "imageRenderer.token=render-only-fixture"}
	before, err := render(t, "", append([]string{"--set-string", "infisical.authMethod=universalAuth"}, fixture...)...)
	if err != nil {
		t.Fatal(err, before)
	}
	after, err := render(t, "", append(nativeArgs(), fixture...)...)
	if err != nil {
		t.Fatal(err, after)
	}
	otherDocuments := func(rendered string) string {
		var kept []string
		for _, doc := range strings.Split(rendered, "---\n") {
			if strings.Contains(doc, "# Source: grafana/templates/infisicalsecret.yaml") {
				continue
			}
			if strings.TrimSpace(doc) != "" {
				kept = append(kept, strings.TrimSpace(doc))
			}
		}
		sort.Strings(kept)
		return strings.Join(kept, "\n---\n")
	}
	if otherDocuments(before) != otherDocuments(after) {
		t.Fatal("native auth migration changed another chart resource")
	}
	for _, want := range []string{
		"kind: InfisicalConnection", "kind: InfisicalAuth", "kind: InfisicalStaticSecret",
		"refreshInterval: 1m", "instantUpdates: false", "serviceAccountTokenAudiences: []",
		"projectSlug: \"home-lab-nb0-k\"", "environmentSlug: \"dev\"",
		"secretPath: \"/apps/observability/grafana\"", "creationPolicy: Orphan",
	} {
		if !strings.Contains(after, want) {
			t.Fatalf("missing preserved native contract %q", want)
		}
	}
}

func TestNativeAuthRetainsExplicitAudience(t *testing.T) {
	out, err := render(t, "templates/infisicalsecret.yaml", append(nativeArgs(), "--set-string", "infisical.kubernetesAuth.serviceAccountTokenAudiences[0]=fixture-audience")...)
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, `serviceAccountTokenAudiences: ["fixture-audience"]`) {
		t.Fatal("explicit TokenRequest audience changed")
	}
}

func TestHomelabReaderMatchesCompletedMigration(t *testing.T) {
	out, err := render(t, "templates/infisicalsecret.yaml")
	if err != nil {
		t.Fatal(err, out)
	}
	for _, want := range []string{
		"apiVersion: secrets.infisical.com/v1beta1", "kind: InfisicalStaticSecret",
		"identityId: \"d05fbb07-7b0c-402a-888d-980b9ac06cde\"",
		"name: \"kubedesk-infisical-reader\"", "namespace: \"platform-secrets\"",
		"serviceAccountTokenAudiences: []", "creationPolicy: Orphan",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("homelab loses completed reader contract %q", want)
		}
	}
	for _, forbidden := range []string{"kind: InfisicalSecret\n", "universalAuth:", "credentialsRef:", "development-tools"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("homelab restored retired auth setting %q", forbidden)
		}
	}
}

func TestPortableDevelopmentKeepsUniversalAuthDefault(t *testing.T) {
	out, err := renderWithProfile(t, "values.development.yaml", "templates/infisicalsecret.yaml")
	if err != nil {
		t.Fatal(err, out)
	}
	for _, want := range []string{"kind: InfisicalSecret", "universalAuth:", "secretName: \"dummy-auth\"", "projectSlug: \"dummy-project\"", "envSlug: \"dummy-env\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("portable development contract missing %q", want)
		}
	}
	for _, forbidden := range []string{"kind: InfisicalAuth", "kubedesk-infisical-reader", "d05fbb07-7b0c-402a-888d-980b9ac06cde", "home-lab-nb0-k"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("portable development inherited homelab identity %q", forbidden)
		}
	}
}
