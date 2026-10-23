package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestDNSManagementRequiresAuthenticatedWrites(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_DNS_MANAGEMENT_ENABLED", "true")
	t.Setenv("KUCHDESK_AGENT_TOKEN", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "KUCHDESK_AGENT_TOKEN") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestChartUsesHelmEnvironmentWithoutHostFiles(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm unavailable")
	}
	args := []string{"template", "agent", "../../chart", "--set", "cluster.id=synthetic", "--set", "cluster.name=Synthetic", "--set", "image.repository=example.invalid/agent", "--set", "image.tag=git-123456789abc-dirty-abcdef012345", "--set", "auth.existingSecret=", "--set-string", "auth.token=synthetic-only"}
	output, err := exec.Command(helm, args...).Output()
	if err != nil {
		t.Fatal("synthetic Helm ENV/Secret chart render failed")
	}
	text := string(output)
	for _, required := range []string{"KUCHDESK_CLUSTER_ID", "KUCHDESK_AGENT_TOKEN", "secretKeyRef:", "kind: Secret", "git-123456789abc-dirty-abcdef012345", "runAsUser: 65532"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing chart contract: %s", required)
		}
	}
	for _, forbidden := range []string{"hostPath:", "KUCHDESK_VALUES_ROOT", "kuchdesk-source", "values-0"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("chart grants direct host file access: %s", forbidden)
		}
	}
	for _, extra := range [][]string{
		{"--set", "configFiles.enabled=true"},
		// Helm applies --set-string after --set regardless of argument order.
		{"--set-string", "auth.token="},
		{"--set", "image.tag=git-123456789abc-dirty-invalid"},
	} {
		if err := exec.Command(helm, append(append([]string{}, args...), extra...)...).Run(); err == nil {
			t.Fatalf("chart accepted invalid override %q", extra[1])
		}
	}
}

func TestDiscoveryRequiresBearerToken(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_MANAGEMENT_ENABLED", "false")
	t.Setenv("KUCHDESK_AGENT_TOKEN", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "KUCHDESK_AGENT_TOKEN") {
		t.Fatal("read-only discovery accepted anonymous access")
	}
}

func TestDNSManagementLoadsCoreDNSTarget(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_DNS_MANAGEMENT_ENABLED", "true")
	t.Setenv("KUCHDESK_AGENT_TOKEN", "test-token")
	t.Setenv("KUCHDESK_COREDNS_NAMESPACE", "dns-system")
	t.Setenv("KUCHDESK_COREDNS_CONFIGMAP", "custom-dns")
	t.Setenv("KUCHDESK_COREDNS_COREFILE_KEY", "CustomCorefile")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DNSManagementEnabled ||
		cfg.CoreDNSNamespace != "dns-system" ||
		cfg.CoreDNSConfigMap != "custom-dns" ||
		cfg.CoreDNSCorefileKey != "CustomCorefile" {
		t.Fatalf("unexpected DNS configuration: %#v", cfg)
	}
}

func TestRejectsLegacyCoreDNSSettings(t *testing.T) {
	setClusterIdentity(t)
	for _, legacy := range []string{"KUCHDESK_COREDNS_CUSTOM_CONFIGMAP", "KUCHDESK_COREDNS_OVERRIDE_KEY"} {
		t.Run(legacy, func(t *testing.T) {
			t.Setenv(legacy, "old")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), legacy) {
				t.Fatalf("legacy setting error = %v", err)
			}
		})
	}
}

func TestManagementRequiresBearerToken(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_MANAGEMENT_ENABLED", "true")
	t.Setenv("KUCHDESK_AGENT_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("management started without bearer token")
	}
	t.Setenv("KUCHDESK_AGENT_TOKEN", "test-token")
	cfg, err := Load()
	if err != nil || !cfg.ManagementEnabled {
		t.Fatalf("management config: %v, %#v", err, cfg)
	}
}

func TestExecRequiresExplicitManagement(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_EXEC_ENABLED", "true")
	t.Setenv("KUCHDESK_MANAGEMENT_ENABLED", "false")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "KUCHDESK_MANAGEMENT_ENABLED") {
		t.Fatalf("exec gate error = %v", err)
	}
	t.Setenv("KUCHDESK_MANAGEMENT_ENABLED", "true")
	t.Setenv("KUCHDESK_AGENT_TOKEN", "test-token")
	cfg, err := Load()
	if err != nil || !cfg.ExecEnabled {
		t.Fatalf("explicit exec config = %#v, %v", cfg, err)
	}
}

func TestPortForwardRequiresExplicitManagement(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_PORT_FORWARD_ENABLED", "true")
	t.Setenv("KUCHDESK_MANAGEMENT_ENABLED", "false")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "KUCHDESK_MANAGEMENT_ENABLED") {
		t.Fatalf("port-forward gate error = %v", err)
	}
	t.Setenv("KUCHDESK_MANAGEMENT_ENABLED", "true")
	t.Setenv("KUCHDESK_AGENT_TOKEN", "test-token")
	cfg, err := Load()
	if err != nil || !cfg.PortForwardEnabled {
		t.Fatalf("explicit port-forward config = %#v, %v", cfg, err)
	}
}

func TestHostBridgeSupportsReadOnlyAgentWithDedicatedBearer(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_MANAGEMENT_ENABLED", "false")
	t.Setenv("KUCHDESK_EXEC_ENABLED", "false")
	t.Setenv("KUCHDESK_PORT_FORWARD_ENABLED", "false")
	t.Setenv("KUCHDESK_HOST_BRIDGE_ENABLED", "true")
	t.Setenv("KUCHDESK_HOST_BRIDGE_URL", "https://host.lima.internal:8181")
	t.Setenv("KUCHDESK_HOST_BRIDGE_TOKEN", "0123456789abcdef0123456789abcdef")
	cfg, err := Load()
	if err != nil || !cfg.HostBridgeEnabled || cfg.ManagementEnabled || cfg.ExecEnabled || cfg.PortForwardEnabled {
		t.Fatal("authenticated host bridge rejected or enabled Kubernetes management")
	}
	for _, missing := range []string{"KUCHDESK_AGENT_TOKEN", "KUCHDESK_HOST_BRIDGE_TOKEN", "KUCHDESK_HOST_BRIDGE_URL"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv(missing, "")
			if _, err := Load(); err == nil {
				t.Fatal("host bridge accepted missing authentication or URL")
			}
		})
	}
}

func TestChartHostBridgeRetainsExistingSecretReferences(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is not installed")
	}
	chart := filepath.Join("..", "..", "chart")
	base := []string{"template", "agent", chart, "--set", "cluster.id=example", "--set", "cluster.name=Example", "--set", "image.repository=example.invalid/agent", "--set", "image.tag=git-123456789abc"}
	render := func(extra ...string) (string, error) {
		command := exec.Command(helm, append(append([]string{}, base...), extra...)...)
		output, err := command.CombinedOutput()
		return string(output), err
	}
	output, err := render()
	if err != nil || !strings.Contains(output, "KUCHDESK_HOST_BRIDGE_ENABLED") || strings.Contains(output, "KUCHDESK_HOST_BRIDGE_TOKEN") {
		t.Fatalf("disabled chart render = %v", err)
	}
	output, err = render("--set", "agent.managementEnabled=false", "--set", "rbac.clusterAdmin=false", "--set", "hostBridge.enabled=true", "--set", "hostBridge.url=https://host.lima.internal:8181", "--set", "hostBridge.tokenSecretName=host-bridge-auth", "--set", "hostBridge.caConfigMapName=host-bridge-ca")
	if err != nil || !strings.Contains(output, "name: \"host-bridge-auth\"") || !strings.Contains(output, "secretKeyRef:") || !strings.Contains(output, "host-bridge-ca") {
		t.Fatalf("enabled chart missing named Secret or CA reference: %v", err)
	}
	if _, err := render("--set", "auth.existingSecret=", "--set-string", "auth.token=synthetic-only", "--set", "agent.managementEnabled=false", "--set", "rbac.clusterAdmin=false", "--set", "hostBridge.enabled=true", "--set", "hostBridge.url=https://host.lima.internal:8181", "--set", "hostBridge.tokenSecretName=host-bridge-auth"); err != nil {
		t.Fatal("host bridge rejected chart-created agent authentication")
	}
	if _, err := render("--set", "hostBridge.token=plaintext-canary"); err == nil {
		t.Fatal("chart accepted a short bridge bearer")
	}
}

func TestChartValuesCreateMatchingAgentSecret(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is not installed")
	}
	chart := filepath.Join("..", "..", "chart")
	args := []string{"template", "agent", chart, "--set", "cluster.id=example", "--set", "cluster.name=Example", "--set", "image.repository=example.invalid/agent", "--set", "image.tag=git-123456789abc", "--set", "auth.existingSecret=", "--set-string", "auth.token=dummy-fixture-token"}
	output, err := exec.Command(helm, args...).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "name: agent-kuchdesk-agent-auth") || !strings.Contains(string(output), "key: \"KUCHDESK_AGENT_TOKEN\"") {
		t.Fatal("agent Pod does not reference its chart-created Secret")
	}
}

func TestClusterIdentityRequired(t *testing.T) {
	for _, missing := range []string{"KUCHDESK_CLUSTER_ID", "KUCHDESK_CLUSTER_NAME"} {
		t.Run(missing, func(t *testing.T) {
			setClusterIdentity(t)
			t.Setenv(missing, " ")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("Load() error = %v; want %s", err, missing)
			}
		})
	}
}

func TestLoadReadsExplicitLocalKubeContext(t *testing.T) {
	setClusterIdentity(t)
	t.Setenv("KUCHDESK_KUBE_CONTEXT", "  colima-team  ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KubeContext != "colima-team" {
		t.Fatalf("context = %q", cfg.KubeContext)
	}
}

func setClusterIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("KUCHDESK_AGENT_TOKEN", "test-token")
	t.Setenv("KUCHDESK_CLUSTER_ID", "test-cluster")
	t.Setenv("KUCHDESK_CLUSTER_NAME", "Test cluster")
}

func TestRESTConfigUsesHostKubeconfigWithoutInClusterCredentials(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	path := writeKubeconfig(t, "colima-test", "https://local.example.test")
	t.Setenv("KUBECONFIG", path)

	if _, err := RESTConfig(Config{}); err == nil {
		t.Fatal("implicit host context accepted")
	}
	restConfig, err := RESTConfig(Config{KubeContext: "colima-test"})
	if err != nil {
		t.Fatal(err)
	}
	if restConfig.Host != "https://local.example.test" {
		t.Fatalf("host = %q", restConfig.Host)
	}
}

func TestRESTConfigSupportsKubeconfigListAndExplicitContext(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	first := writeKubeconfig(t, "colima-first", "https://first.example.test")
	second := writeKubeconfig(t, "colima-second", "https://second.example.test")

	restConfig, err := RESTConfig(Config{
		Kubeconfig:  strings.Join([]string{first, second}, string(os.PathListSeparator)),
		KubeContext: "colima-second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if restConfig.Host != "https://second.example.test" {
		t.Fatalf("host = %q", restConfig.Host)
	}
}

func writeKubeconfig(t *testing.T, name, server string) string {
	t.Helper()
	config := clientcmdapi.NewConfig()
	config.Clusters[name] = &clientcmdapi.Cluster{Server: server}
	config.AuthInfos[name] = &clientcmdapi.AuthInfo{Token: "test-token"}
	config.Contexts[name] = &clientcmdapi.Context{Cluster: name, AuthInfo: name}
	config.CurrentContext = name
	path := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBaseProjectNameIsConfigurableDisplayOnly(t *testing.T) {
	setClusterIdentity(t)
	for _, name := range []string{"", "Another Base Project", "ابزارها"} {
		t.Setenv("KUCHDESK_BASE_PROJECT_NAME", name)
		cfg, e := Load()
		if e != nil {
			t.Fatal(e)
		}
		want := name
		if want == "" {
			want = "Kubedesk Platform"
		}
		if cfg.BaseProjectName != want {
			t.Fatal("display name mismatch")
		}
		if cfg.BaseProjectID != "" {
			t.Fatal("name selected a project")
		}
	}
	for _, name := range []string{"  ", "bad\nname"} {
		t.Setenv("KUCHDESK_BASE_PROJECT_NAME", name)
		if _, e := Load(); e == nil {
			t.Fatal("invalid name accepted")
		}
	}
}

func TestChartHostBridgeCreatesMatchingSecretAndTrustConfigMap(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm unavailable")
	}
	file := filepath.Join(t.TempDir(), "values.local.yaml")
	values := "auth:\n  existingSecret: \"\"\n  token: synthetic-agent-bearer\nhostBridge:\n  enabled: true\n  url: https://host.lima.internal:8181\n  token: " + strings.Repeat("b", 32) + "\n  caPEM: synthetic-public-certificate\nagent:\n  managementEnabled: false\nrbac:\n  clusterAdmin: false\n"
	if err := os.WriteFile(file, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"template", "agent", "../../chart", "-f", file, "--set", "cluster.id=synthetic", "--set", "cluster.name=Synthetic", "--set", "image.repository=example.invalid/agent", "--set", "image.tag=git-123456789abc"}
	output, err := exec.Command(helm, args...).Output()
	if err != nil {
		t.Fatal("synthetic bridge chart render failed")
	}
	text := string(output)
	for _, want := range []string{"kind: Secret", "name: agent-kuchdesk-agent-bridge-auth", "name: \"agent-kuchdesk-agent-bridge-auth\"", "KUCHDESK_HOST_BRIDGE_TOKEN:", "kind: ConfigMap", "name: agent-kuchdesk-agent-bridge-ca", "name: \"agent-kuchdesk-agent-bridge-ca\"", "ca.crt: \"synthetic-public-certificate\"", "value: /etc/kuchdesk/host-bridge/ca.crt", "checksum/host-bridge-ca:"} {
		if !strings.Contains(text, want) {
			t.Fatal("bridge resources and Pod references do not match")
		}
	}
	if strings.Contains(text, "value: \""+strings.Repeat("b", 32)+"\"") || strings.Contains(text, "hostPath:") || strings.Contains(text, "name: cluster-admin") {
		t.Fatal("bridge exposed credentials, mounted host files or expanded Kubernetes RBAC")
	}
	for _, override := range []string{"hostBridge.tokenSecretName=existing", "hostBridge.caConfigMapName=existing"} {
		if err := exec.Command(helm, append(append([]string{}, args...), "--set", override)...).Run(); err == nil {
			t.Fatal("chart accepted conflicting generated and existing bridge resources")
		}
	}
}
