package chartvalues

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEditPreservesOtherValuesAndComments(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab/apps/platform/technitium/values.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	input := "# top comment\ndomain: dns.local.dev\nlanService:\n  enabled: true # keep this\n  type: LoadBalancer\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Set(root, "technitium", "values.yaml", []string{"lanService", "type"}, "ClusterIP"); err != nil {
		t.Fatal(err)
	}
	data, _, err := Read(root, "technitium", "values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		Domain string `yaml:"domain"`
		LAN    struct {
			Enabled bool   `yaml:"enabled"`
			Type    string `yaml:"type"`
		} `yaml:"lanService"`
	}
	if err := yaml.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Domain != "dns.local.dev" || !actual.LAN.Enabled || actual.LAN.Type != "ClusterIP" || !strings.Contains(string(data), "# top comment") || !strings.Contains(string(data), "# keep this") {
		t.Fatal("edit changed unrelated YAML")
	}
	if err := Set(root, "technitium", "values.yaml", []string{"missing"}, "true"); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := Set(root, "technitium", "values.yaml", []string{"adminPassword"}, "synthetic-only"); err == nil {
		t.Fatal("credential edit in tracked values accepted")
	}
}

func TestPrivateValuesAreRedactedAndRemainPrivate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab/apps/platform/technitium/values.local.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("adminPassword: dummy-synthetic-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inspection, err := Inspect(root, "technitium", "values.local.yaml")
	if err != nil || strings.Contains(inspection, "dummy-synthetic-only") || !strings.Contains(inspection, "[redacted]") {
		t.Fatal("private value was exposed or inspection failed")
	}
	if err := Set(root, "technitium", "values.local.yaml", []string{"adminPassword"}, "new-synthetic-only"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("private values permissions changed")
	}
	if _, _, err := Read(root, "finance", "values.local.yaml"); err == nil {
		t.Fatal("unknown service was accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(root, "technitium", "values.local.yaml"); err == nil {
		t.Fatal("world-readable private values were accepted")
	}
}

func TestCatalogUsesExistingCanonicalFiles(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for service, names := range Files {
		for _, relative := range names {
			name := filepath.Base(relative)
			if strings.HasSuffix(name, ".local.yaml") {
				if _, err := os.Stat(filepath.Join(root, strings.TrimSuffix(relative, ".local.yaml")+".local.example.yaml")); err != nil {
					t.Fatalf("%s missing local example: %v", service, err)
				}
				continue
			}
			if _, _, err := Read(root, service, name); err != nil {
				t.Fatalf("%s/%s: %v", service, name, err)
			}
			if _, err := Inspect(root, service, name); err != nil {
				t.Fatalf("%s/%s is not inspectable YAML: %v", service, name, err)
			}
		}
	}
}

func TestHostBridgeValuesArePrivateAndRedacted(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "host-agent/bridge-values.local.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("token: synthetic-private-bearer\nlisten: 127.0.0.1:8181\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := Inspect(root, "host-bridge", "bridge-values.local.yaml")
	if err != nil || strings.Contains(string(data), "synthetic-private-bearer") {
		t.Fatal("Host Bridge private YAML was not redacted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(root, "host-bridge", "bridge-values.local.yaml"); err == nil {
		t.Fatal("unsafe Host Bridge YAML permissions accepted")
	}
}

func TestCanonicalValuesCannotEscapeProjectRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "lab/apps/platform"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "values.yaml"), []byte("domain: dummy.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "lab/apps/platform/technitium")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(root, "technitium", "values.yaml"); err == nil {
		t.Fatal("values path escaped project root")
	}
}

func TestCatalogCoversEveryExistingChartValuesFile(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	listed := make(map[string]bool)
	for _, names := range Files {
		for _, name := range names {
			listed[name] = true
		}
	}
	for _, dir := range []string{"lab/apps", "kuchdesk-agent/chart"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || entry.Name() != "values.yaml" {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if !listed[relative] {
				t.Errorf("chart values missing from direct YAML catalog: %s", relative)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMultiDocumentEditTargetsOnlySelectedResource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab/apps/observability/host-dns-monitoring/dns-probes.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("kind: Probe\nmetadata:\n  name: first\n---\nkind: Probe\nmetadata:\n  name: second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Set(root, "dns-monitoring", "dns-probes.yaml", []string{"1", "metadata", "name"}, "changed"); err != nil {
		t.Fatal(err)
	}
	result, err := Inspect(root, "dns-monitoring", "dns-probes.yaml")
	if err != nil || !strings.Contains(result, "name: first") || !strings.Contains(result, "name: changed") {
		t.Fatal("multi-document edit changed the wrong resource")
	}
	if err := Set(root, "dns-monitoring", "dns-probes.yaml", []string{"metadata", "name"}, "wrong"); err == nil {
		t.Fatal("multi-document edit omitted document index")
	}
}
