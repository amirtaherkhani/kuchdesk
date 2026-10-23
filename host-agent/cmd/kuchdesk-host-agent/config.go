package main

import (
	"flag"
	"fmt"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/localruntime"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type config struct {
	CheckTarget   bool
	TargetIP      string
	Zone          string
	KubeContext   string
	KubeAPIServer string
	Namespace     string
	Service       string
	AdminUser     string
	APIPort       int
	Kubectl       string
	TTL           int
	Timeout       time.Duration
	ValuesRoot    string
}

func parseConfig(args []string) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("kuchdesk-host-agent", flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: kuchdesk-host-agent [DNS options]\n       kuchdesk-host-agent grafana-admin-password --values-root PROJECT_ROOT\n       kuchdesk-host-agent grafana-admin-password-reset --kube-context CONTEXT [--user-id ID]")
		flags.PrintDefaults()
	}
	flags.BoolVar(&cfg.CheckTarget, "check-target", false, "validate and print the explicit DNS target without changing Technitium")
	flags.StringVar(&cfg.TargetIP, "target-ip", "", "verified Colima ingress IPv4 address; required for reconciliation")
	flags.StringVar(&cfg.Zone, "zone", "", "DNS zone to reconcile, for example local.dev")
	flags.StringVar(&cfg.KubeContext, "kube-context", "", "Kubernetes context containing Technitium")
	flags.StringVar(&cfg.KubeAPIServer, "kube-api-server", "", "optional verified HTTPS loopback Kubernetes API endpoint; normal certificate verification remains enabled")
	flags.StringVar(&cfg.Namespace, "namespace", "technitium", "Technitium Kubernetes namespace")
	flags.StringVar(&cfg.Service, "service", "technitium", "Technitium Kubernetes Service name")
	flags.StringVar(&cfg.AdminUser, "admin-user", "admin", "Technitium admin username")
	flags.IntVar(&cfg.APIPort, "api-port", 5380, "Technitium admin Service port")
	flags.StringVar(&cfg.Kubectl, "kubectl", "kubectl", "kubectl executable or absolute path")
	flags.IntVar(&cfg.TTL, "ttl", 30, "wildcard A record TTL in seconds")
	flags.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "maximum time for one reconciliation")
	flags.StringVar(&cfg.ValuesRoot, "values-root", "", "absolute KuchDesk source root containing canonical chart YAML")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	cfg.Zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(cfg.Zone), "."))
	if !validDNSName(cfg.Zone) {
		return config{}, fmt.Errorf("-zone must be a valid DNS zone")
	}
	if strings.TrimSpace(cfg.KubeContext) == "" {
		return config{}, fmt.Errorf("-kube-context is required to avoid changing clusters when the current context changes")
	}
	if _, err := localruntime.Profile(cfg.KubeContext); err != nil {
		return config{}, err
	}
	if cfg.TargetIP != "" {
		ip := net.ParseIP(cfg.TargetIP)
		if ip == nil || ip.To4() == nil || (!ip.IsGlobalUnicast() && !ip.IsLoopback()) {
			return config{}, fmt.Errorf("-target-ip must be a usable IPv4 address")
		}
	}
	if !validDNSName(cfg.Namespace) || strings.Contains(cfg.Namespace, ".") ||
		!validDNSName(cfg.Service) || strings.Contains(cfg.Service, ".") {
		return config{}, fmt.Errorf("namespace and service must be DNS labels")
	}
	if strings.TrimSpace(cfg.AdminUser) == "" {
		return config{}, fmt.Errorf("admin-user cannot be empty")
	}
	if cfg.APIPort < 1 || cfg.APIPort > 65535 {
		return config{}, fmt.Errorf("-api-port must be between 1 and 65535")
	}
	if cfg.KubeAPIServer != "" {
		endpoint, err := url.Parse(cfg.KubeAPIServer)
		if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() != "127.0.0.1" || endpoint.Port() == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return config{}, fmt.Errorf("-kube-api-server must be an HTTPS 127.0.0.1 endpoint with an explicit port")
		}
		if port, err := strconv.Atoi(endpoint.Port()); err != nil || port < 1 || port > 65535 {
			return config{}, fmt.Errorf("-kube-api-server port is invalid")
		}
	}
	if strings.TrimSpace(cfg.Kubectl) == "" {
		return config{}, fmt.Errorf("-kubectl cannot be empty")
	}
	if cfg.TTL < 1 || cfg.TTL > 86400 {
		return config{}, fmt.Errorf("-ttl must be between 1 and 86400 seconds")
	}
	if cfg.Timeout <= 0 {
		return config{}, fmt.Errorf("-timeout must be positive")
	}
	return cfg, nil
}

func validDNSName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func (cfg config) recordName() string {
	return "*." + cfg.Zone
}

func (cfg config) kubectlArgs(args ...string) []string {
	base := []string{"--context", cfg.KubeContext, "-n", cfg.Namespace}
	if cfg.KubeAPIServer != "" {
		base = append(base, "--server", cfg.KubeAPIServer)
	}
	return append(base, args...)
}
