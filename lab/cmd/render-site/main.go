package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type siteProfile struct {
	Domain string `json:"domain"`
	Hosts  struct {
		Grafana string `json:"grafana"`
	} `json:"hosts"`
	TLS struct {
		SecretName string `json:"secretName,omitempty"`
		Issuer     struct {
			Type           string `json:"type"`
			Name           string `json:"name"`
			SelfSignedName string `json:"selfSignedName"`
			CASecretName   string `json:"caSecretName"`
			CANamespace    string `json:"caNamespace"`
			CACommonName   string `json:"caCommonName"`
		} `json:"issuer"`
		CertificateNamespaces    []string `json:"certificateNamespaces"`
		DefaultTLSStoreNamespace string   `json:"defaultTlsStoreNamespace"`
	} `json:"tls"`
}

type output struct {
	resources map[string]any
	summary   map[string]any
}

var dnsLabel = regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")

func validName(value, field string, requireDot bool) error {
	if value == "" || len(value) > 253 || (requireDot && !strings.Contains(value, ".")) {
		return fmt.Errorf("%s must be a nonempty lowercase DNS name", field)
	}
	for _, label := range strings.Split(value, ".") {
		if !dnsLabel.MatchString(label) {
			return fmt.Errorf("%s must be a lowercase DNS name", field)
		}
	}
	return nil
}

func resource(apiVersion, kind, name, namespace string, spec any) map[string]any {
	metadata := map[string]any{"name": name}
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	return map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": metadata, "spec": spec}
}

func render(profile siteProfile, domainOverride string) (output, error) {
	domain := profile.Domain
	if domainOverride != "" {
		domain = domainOverride
	}
	if err := validName(domain, "domain", true); err != nil {
		return output{}, err
	}
	for service, host := range map[string]string{"grafana": profile.Hosts.Grafana} {
		if err := validName(host, "hosts."+service, false); err != nil {
			return output{}, err
		}
		if strings.Contains(host, ".") {
			return output{}, fmt.Errorf("hosts.%s must be one label for wildcard TLS", service)
		}
	}
	issuer := profile.TLS.Issuer
	if issuer.Type != "private-ca" && issuer.Type != "existing" {
		return output{}, errors.New("tls.issuer.type must be private-ca or existing")
	}
	if err := validName(issuer.Name, "tls.issuer.name", false); err != nil {
		return output{}, err
	}
	namespaces := profile.TLS.CertificateNamespaces
	if len(namespaces) == 0 {
		return output{}, errors.New("certificateNamespaces must be a nonempty list")
	}
	seen := make(map[string]bool)
	for _, namespace := range namespaces {
		if err := validName(namespace, "certificate namespace", false); err != nil {
			return output{}, err
		}
		if seen[namespace] {
			return output{}, errors.New("certificateNamespaces must not contain duplicates")
		}
		seen[namespace] = true
	}
	defaultNS := profile.TLS.DefaultTLSStoreNamespace
	if err := validName(defaultNS, "defaultTlsStoreNamespace", false); err != nil {
		return output{}, err
	}
	if !seen[defaultNS] {
		return output{}, errors.New("default TLSStore namespace needs its own Certificate and Secret")
	}
	secretName := profile.TLS.SecretName
	if secretName == "" {
		// Preserve the naming convention of profiles created before tls.secretName.
		secretName = strings.ReplaceAll(domain, ".", "-") + "-tls"
	}
	if err := validName(secretName, "TLS Secret name", false); err != nil {
		return output{}, err
	}
	grafanaHost := profile.Hosts.Grafana + "." + domain
	resources := make(map[string]any)
	var caSecretName, caNamespace any
	if issuer.Type == "private-ca" {
		for field, value := range map[string]string{
			"selfSignedName": issuer.SelfSignedName,
			"caSecretName":   issuer.CASecretName,
			"caNamespace":    issuer.CANamespace,
		} {
			if err := validName(value, field, false); err != nil {
				return output{}, err
			}
		}
		if !seen[issuer.CANamespace] {
			return output{}, errors.New("CA namespace must be present in certificateNamespaces")
		}
		if issuer.CACommonName == "" {
			return output{}, errors.New("caCommonName is required")
		}
		caSecretName, caNamespace = issuer.CASecretName, issuer.CANamespace
		resources["00-selfsigned-issuer.json"] = resource("cert-manager.io/v1", "ClusterIssuer", issuer.SelfSignedName, "", map[string]any{"selfSigned": map[string]any{}})
		resources["01-ca-certificate.json"] = resource("cert-manager.io/v1", "Certificate", issuer.CASecretName, issuer.CANamespace, map[string]any{
			"secretName": issuer.CASecretName,
			"issuerRef":  map[string]any{"name": issuer.SelfSignedName, "kind": "ClusterIssuer"},
			"commonName": issuer.CACommonName, "isCA": true,
			"duration": "87600h", "renewBefore": "720h",
			"privateKey": map[string]any{"algorithm": "ECDSA", "size": 256},
			"usages":     []string{"cert sign", "crl sign", "digital signature"},
		})
		resources["02-ca-issuer.json"] = resource("cert-manager.io/v1", "ClusterIssuer", issuer.Name, "", map[string]any{"ca": map[string]any{"secretName": issuer.CASecretName}})
	}
	for _, namespace := range namespaces {
		spec := map[string]any{
			"secretName": secretName,
			"issuerRef":  map[string]any{"name": issuer.Name, "kind": "ClusterIssuer"},
			"commonName": domain, "dnsNames": []string{domain, "*." + domain},
			"privateKey": map[string]any{"algorithm": "ECDSA", "size": 256, "rotationPolicy": "Always"},
		}
		if issuer.Type == "private-ca" {
			spec["duration"] = "8760h"
			spec["renewBefore"] = "720h"
		}
		resources["10-certificate-"+namespace+".json"] = resource("cert-manager.io/v1", "Certificate", secretName, namespace, spec)
	}
	resources["20-default-tlsstore.json"] = resource("traefik.io/v1alpha1", "TLSStore", "default", defaultNS, map[string]any{"defaultCertificate": map[string]any{"secretName": secretName}})
	resources["30-dns-blackbox-config.json"] = map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{"name": "dns-blackbox-config", "namespace": "observability"},
		"data": map[string]any{"blackbox.yml": fmt.Sprintf(`modules:
  dns_site:
    prober: dns
    timeout: 5s
    dns:
      preferred_ip_protocol: ip4
      query_name: %s
      query_type: A
      valid_rcodes: [NOERROR]
      validate_answer_rrs:
        fail_if_none_matches_regexp: ['\sIN\sA\s']
  dns_recursive:
    prober: dns
    timeout: 5s
    dns:
      preferred_ip_protocol: ip4
      query_name: example.com
      query_type: A
      valid_rcodes: [NOERROR]
      validate_answer_rrs:
        fail_if_none_matches_regexp: ['\sIN\sA\s']
`, grafanaHost)},
	}
	summary := map[string]any{
		"domain": domain, "secretName": secretName, "certificateNamespaces": namespaces,
		"caSecretName": caSecretName, "caNamespace": caNamespace,
		"grafanaUrl": "https://" + grafanaHost,
	}
	return output{resources: resources, summary: summary}, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func writeOutput(dir string, result output) error {
	marker := filepath.Join(dir, ".kuchdesk-site-render")
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		if _, err := os.Stat(marker); err != nil {
			return fmt.Errorf("%s is not a site-render output directory", dir)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	manifests := filepath.Join(dir, "manifests")
	if err := os.MkdirAll(manifests, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(manifests)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			if err := os.Remove(filepath.Join(manifests, entry.Name())); err != nil {
				return err
			}
		}
	}
	for name, value := range result.resources {
		if err := writeJSON(filepath.Join(manifests, name), value); err != nil {
			return err
		}
	}
	for _, name := range []string{"grafana-values.json"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := writeJSON(filepath.Join(dir, "site-summary.json"), result.summary); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte("Generated by lab/cmd/render-site; do not edit.\n"), 0o644)
}

func summaryField(dir, key string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "site-summary.json"))
	if err != nil {
		return "", err
	}
	var summary map[string]any
	if err := json.Unmarshal(data, &summary); err != nil {
		return "", err
	}
	value, ok := summary[key]
	if !ok {
		return "", fmt.Errorf("unknown summary field %q", key)
	}
	switch typed := value.(type) {
	case string:
		return typed, nil
	case []any:
		names := make([]string, 0, len(typed))
		for _, item := range typed {
			name, ok := item.(string)
			if !ok {
				return "", fmt.Errorf("summary field %q contains a non-string value", key)
			}
			names = append(names, name)
		}
		return strings.Join(names, " "), nil
	default:
		return "", fmt.Errorf("summary field %q has no printable value", key)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "config" {
		return runConfig(args[1:], os.Stdout)
	}
	flags := flag.NewFlagSet("render-site", flag.ContinueOnError)
	profilePath := flags.String("profile", "site.json", "site profile")
	domain := flags.String("domain", "", "override the profile's DNS domain")
	outputPath := flags.String("output", ".generated", "render output directory")
	get := flags.String("get", "", "print one summary field without writing output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *get != "" {
		value, err := summaryField(*outputPath, *get)
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil
	}
	data, err := os.ReadFile(*profilePath)
	if err != nil {
		return err
	}
	var profile siteProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return err
	}
	result, err := render(profile, *domain)
	if err != nil {
		return err
	}
	if err := writeOutput(*outputPath, result); err != nil {
		return err
	}
	fmt.Printf("Rendered %s to %s\n", result.summary["domain"], *outputPath)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
