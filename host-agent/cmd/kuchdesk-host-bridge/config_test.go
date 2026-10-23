package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateValuesSupplyBridgeWithoutEnvironment(t *testing.T) {
	t.Setenv("KUCHDESK_HOST_BRIDGE_ENABLED", "false")
	t.Setenv("KUCHDESK_HOST_BRIDGE_TOKEN", "wrong-environment-value")
	path := filepath.Join(t.TempDir(), "bridge-values.local.yaml")
	token := strings.Repeat("s", 32)
	data := "listen: 127.0.0.1:8181\ntlsCert: cert.pem\ntlsKey: key.pem\ntoken: " + token + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig([]string{"-values", path})
	if err != nil || cfg.Token != token || cfg.Listen != "127.0.0.1:8181" || cfg.TLSCert != filepath.Join(filepath.Dir(path), "cert.pem") || cfg.TLSKey != filepath.Join(filepath.Dir(path), "key.pem") {
		t.Fatal("private YAML did not provide bridge configuration")
	}
	if _, err := loadConfig([]string{"-values", path, "-listen", "127.0.0.1:9000"}); err == nil {
		t.Fatal("YAML mode accepted conflicting CLI values")
	}
	withRelativeRoot := data + "projectRoot: relative/path\n"
	if err := os.WriteFile(path, []byte(withRelativeRoot), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig([]string{"-values", path}); err == nil {
		t.Fatal("relative projectRoot accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig([]string{"-values", path}); err == nil {
		t.Fatal("publicly readable bridge values accepted")
	}
}

func TestBridgeRejectsIncompleteOrInvalidPrivateValuesWithoutDisclosure(t *testing.T) {
	t.Setenv("KUCHDESK_HOST_BRIDGE_ENABLED", "true")
	t.Setenv("KUCHDESK_HOST_BRIDGE_TOKEN", strings.Repeat("e", 32))
	path := filepath.Join(t.TempDir(), "bridge-values.local.yaml")
	secret := strings.Repeat("s", 32)
	base := "listen: 127.0.0.1:8181\ntlsCert: cert.pem\ntlsKey: key.pem\n"
	for _, data := range []string{
		base,
		base + "token: short\n",
		base + "token: " + secret + "\nunknown: true\n",
		base + "token: " + secret + "\nforwardingRecovery: true\n",
		base + "token: [" + secret + "]\n",
		base + "token: " + secret + "\n---\nlisten: 127.0.0.1:9000\n",
		strings.ReplaceAll(base, "127.0.0.1:8181", ":8181") + "token: " + secret + "\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig([]string{"-values", path})
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("invalid YAML accepted or its contents disclosed")
		}
	}
}

func TestLegacyBridgeStillRequiresExplicitEnablement(t *testing.T) {
	t.Setenv("KUCHDESK_HOST_BRIDGE_ENABLED", "")
	t.Setenv("KUCHDESK_HOST_BRIDGE_TOKEN", strings.Repeat("e", 32))
	args := []string{"-tls-cert", "cert.pem", "-tls-key", "key.pem"}
	if _, err := loadConfig(args); err == nil {
		t.Fatal("legacy network listener implicitly enabled")
	}
	t.Setenv("KUCHDESK_HOST_BRIDGE_ENABLED", "true")
	if _, err := loadConfig(args); err != nil {
		t.Fatal("explicit legacy bridge configuration rejected")
	}
}
