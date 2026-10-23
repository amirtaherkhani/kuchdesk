package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	filePath := filepath.Join(append([]string{"testdata"}, parts...)...)
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseModuleExamples(t *testing.T) {
	for _, item := range []struct {
		file      string
		id        string
		ownership string
	}{
		{"managed.example.yaml", "example-managed-module", "managed"},
		{"external.example.yaml", "external-example-app", "external"},
	} {
		module, err := ParseServiceModule(fixture(t, "service-modules", item.file))
		if err != nil {
			t.Fatalf("%s: %v", item.file, err)
		}
		if module.APIVersion != APIVersion || module.Module.ID != item.id || module.Module.Ownership != item.ownership {
			t.Fatalf("%s: unexpected parsed module: %#v", item.file, module.Module)
		}
		if len(module.Components) != 1 {
			t.Fatalf("%s: expected one component", item.file)
		}
	}
}

func TestParseInstallationProfileExample(t *testing.T) {
	profile, err := ParseInstallationProfile(fixture(t, "installation-profiles", "local-single-node.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.APIVersion != APIVersion || profile.Profile.Target != "local-single-node" || len(profile.Installations) != 1 {
		t.Fatalf("unexpected profile: %#v", profile)
	}
}

func TestParseModuleRejectsUnknownFieldAndVersion(t *testing.T) {
	data := fixture(t, "service-modules", "managed.example.yaml")
	withPassword := strings.Replace(string(data), "    observability:", "    password: raw-secret\n    observability:", 1)
	if _, err := ParseServiceModule([]byte(withPassword)); err == nil {
		t.Fatal("expected unknown raw secret field to be rejected")
	}

	wrongVersion := strings.Replace(string(data), APIVersion, "catalog.kuchdesk.io/v2", 1)
	if _, err := ParseServiceModule([]byte(wrongVersion)); err == nil {
		t.Fatal("expected unsupported API version to be rejected")
	}
}
