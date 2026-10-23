package freshprofile_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	out, err := exec.Command("yq", "eval", "-o=json", ".", path).Output()
	if err != nil {
		t.Fatal("cannot parse direct YAML profile:", path)
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func field(t *testing.T, object map[string]any, keys ...string) any {
	t.Helper()
	var value any = object
	for _, key := range keys {
		next, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("missing profile field %s", key)
		}
		value = next[key]
	}
	return value
}

func TestFreshSiteAndChartsAgree(t *testing.T) {
	raw, err := os.ReadFile("../../site.json")
	if err != nil {
		t.Fatal(err)
	}
	var site map[string]any
	if err := json.Unmarshal(raw, &site); err != nil {
		t.Fatal(err)
	}
	colima := readYAML(t, "../../colima-k3s.example.yaml")
	if field(t, colima, "runtime") != "containerd" || field(t, colima, "kubernetes", "enabled") != true || len(field(t, colima, "kubernetes", "k3sArgs").([]any)) != 0 || field(t, colima, "network", "address") != true {
		t.Fatal("fresh Colima profile must provide reachable K3s with bundled Traefik")
	}
	grafana := readYAML(t, "../../apps/observability/grafana/values.homelab.yaml")
	host := field(t, site, "hosts", "grafana").(string) + "." + site["domain"].(string)
	ingress := field(t, grafana, "ingress").(map[string]any)
	if ingress["hosts"].([]any)[0] != host || ingress["tls"].([]any)[0].(map[string]any)["secretName"] != field(t, site, "tls", "secretName") || field(t, grafana, "persistence", "storageClassName") != "local-path" {
		t.Fatal("Grafana host, TLS Secret, or new storage class differs from the site")
	}
	if field(t, site, "tls", "defaultTlsStoreNamespace") != "kube-system" {
		t.Fatal("K3s Traefik TLSStore must be in kube-system")
	}
	technitium := readYAML(t, "../../apps/platform/technitium/values.yaml")
	if technitium["domain"] != "dns."+site["domain"].(string) || technitium["storageClassName"] != "local-path" {
		t.Fatal("Technitium domain or new storage class differs from the site")
	}
}
