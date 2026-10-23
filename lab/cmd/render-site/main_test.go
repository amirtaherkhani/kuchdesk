package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadProfile(t *testing.T) siteProfile {
	t.Helper()
	data, err := os.ReadFile("../../site.json")
	if err != nil {
		t.Fatal(err)
	}
	var profile siteProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", value)
	}
	return result
}

func assertTLSName(t *testing.T, result output, namespaces []string, want string) {
	t.Helper()
	if result.summary["secretName"] != want {
		t.Fatalf("unexpected Secret: %v; want %s", result.summary["secretName"], want)
	}
	for _, namespace := range namespaces {
		certificate := object(t, result.resources["10-certificate-"+namespace+".json"])
		if object(t, certificate["metadata"])["name"] != want || object(t, certificate["spec"])["secretName"] != want {
			t.Fatalf("Certificate in %s does not use %s", namespace, want)
		}
	}
	storeSpec := object(t, object(t, result.resources["20-default-tlsstore.json"])["spec"])
	if object(t, storeSpec["defaultCertificate"])["secretName"] != want {
		t.Fatal("TLSStore does not use the selected certificate name")
	}

}

func TestCurrentSiteAndFallbackCertificate(t *testing.T) {
	profile := loadProfile(t)
	result, err := render(profile, "")
	if err != nil {
		t.Fatal(err)
	}
	assertTLSName(t, result, profile.TLS.CertificateNamespaces, "kubedesk-local-tls")
	storeSpec := object(t, object(t, result.resources["20-default-tlsstore.json"])["spec"])
	certSpec := object(t, object(t, result.resources["10-certificate-kube-system.json"])["spec"])
	if object(t, storeSpec["defaultCertificate"])["secretName"] != certSpec["secretName"] {
		t.Fatal("fallback TLSStore has no matching Certificate Secret")
	}
	config := object(t, object(t, result.resources["30-dns-blackbox-config.json"])["data"])["blackbox.yml"].(string)
	if !strings.Contains(config, "query_name: grafana.local.dev") {
		t.Fatalf("DNS probe does not query the rendered site: %s", config)
	}
}

func TestAlternateDomainAndExistingIssuer(t *testing.T) {
	profile := loadProfile(t)
	profile.TLS.SecretName = "" // Older profiles keep their domain-derived name.
	profile.TLS.Issuer.Type = "existing"
	profile.TLS.Issuer.Name = "acme-dns01"
	result, err := render(profile, "example.internal")
	if err != nil {
		t.Fatal(err)
	}
	if result.summary["secretName"] != "example-internal-tls" {
		t.Fatalf("unexpected Secret: %v", result.summary["secretName"])
	}
	if _, exists := result.resources["00-selfsigned-issuer.json"]; exists {
		t.Fatal("existing issuer profile generated a private CA")
	}
	certSpec := object(t, object(t, result.resources["10-certificate-kube-system.json"])["spec"])
	if _, exists := certSpec["duration"]; exists {
		t.Fatal("external issuer has an incompatible fixed duration")
	}
	data, err := json.Marshal([]any{result.resources, result.summary})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "local.dev") {
		t.Fatal("alternate domain retained the old site name")
	}
}

func TestRejectsMissingFallbackAndMultilevelHost(t *testing.T) {
	profile := loadProfile(t)
	profile.TLS.CertificateNamespaces = []string{"platform-system", "observability"}
	if _, err := render(profile, ""); err == nil || !strings.Contains(err.Error(), "default TLSStore namespace") {
		t.Fatalf("expected TLSStore namespace error, got %v", err)
	}
	profile = loadProfile(t)
	profile.Hosts.Grafana = "sub.grafana"
	if _, err := render(profile, ""); err == nil || !strings.Contains(err.Error(), "one label") {
		t.Fatalf("expected wildcard host error, got %v", err)
	}
}

func TestRerenderRemovesStaleCAResources(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "site")
	profile := loadProfile(t)
	initial, err := render(profile, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeOutput(dir, initial); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"grafana-values.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	profile.TLS.Issuer.Type = "existing"
	profile.TLS.Issuer.Name = "acme-dns01"
	profile.TLS.SecretName = ""
	alternate, err := render(profile, "example.internal")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeOutput(dir, alternate); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifests", "00-selfsigned-issuer.json")); !os.IsNotExist(err) {
		t.Fatalf("stale CA manifest survived rerender: %v", err)
	}
	for _, name := range []string{"grafana-values.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("stale chart overlay survived rerender: %s", name)
		}
	}
	value, err := summaryField(dir, "secretName")
	if err != nil {
		t.Fatal(err)
	}
	if value != "example-internal-tls" {
		t.Fatalf("summary returned profile default instead of rendered value: %s", value)
	}
}

func TestLegacyProfileKeepsDomainDerivedTLSName(t *testing.T) {
	profile := loadProfile(t)
	profile.TLS.SecretName = ""
	for _, test := range []struct{ domain, want string }{
		{"", "local-dev-tls"},
		{"example.internal", "example-internal-tls"},
	} {
		t.Run(test.want, func(t *testing.T) {
			result, err := render(profile, test.domain)
			if err != nil {
				t.Fatal(err)
			}
			assertTLSName(t, result, profile.TLS.CertificateNamespaces, test.want)
		})
	}
}

func TestExplicitTLSNameSurvivesDomainOverride(t *testing.T) {
	profile := loadProfile(t)
	profile.TLS.SecretName = "custom-certificate"
	profile.TLS.Issuer.Type = "existing"
	profile.TLS.Issuer.Name = "acme-dns01"
	result, err := render(profile, "example.internal")
	if err != nil {
		t.Fatal(err)
	}
	assertTLSName(t, result, profile.TLS.CertificateNamespaces, "custom-certificate")
	certificate := object(t, result.resources["10-certificate-kube-system.json"])
	if object(t, certificate["spec"])["commonName"] != "example.internal" {
		t.Fatal("explicit certificate name suppressed the domain override")
	}
}

func TestInvalidExplicitTLSNameFailsClosed(t *testing.T) {
	for _, name := range []string{"Bad_Name", "-invalid", "name with spaces", strings.Repeat("a", 254)} {
		profile := loadProfile(t)
		profile.TLS.SecretName = name
		if _, err := render(profile, ""); err == nil || !strings.Contains(err.Error(), "TLS Secret name") {
			t.Fatalf("invalid name %q: expected TLS name error, got %v", name, err)
		}
	}
}
