package main

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"
)

type config struct {
	CheckTarget   bool
	InterfaceName string
	TargetIP      string
	Zone          string
	KubeContext   string
	Namespace     string
	Service       string
	AdminSecret   string
	PasswordKey   string
	AdminUser     string
	APIPort       int
	Kubectl       string
	TTL           int
	Timeout       time.Duration
}

func parseConfig(args []string) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("kuchdesk-host-agent", flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: kuchdesk-host-agent [DNS options]\n       kuchdesk-host-agent admin-login [--project-root /absolute/project]\n       kuchdesk-host-agent grafana-admin-password --kube-context CONTEXT\n       kuchdesk-host-agent grafana-admin-password-reset --kube-context CONTEXT [--user-id ID]")
		flags.PrintDefaults()
	}
	flags.BoolVar(&cfg.CheckTarget, "check-target", false, "resolve and print the DNS target without changing Technitium")
	flags.StringVar(&cfg.InterfaceName, "interface", "", "LAN interface override; default is the operating system's default route interface")
	flags.StringVar(&cfg.TargetIP, "target-ip", "", "explicit IPv4 address for the DNS wildcard; default is the selected interface's private address")
	flags.StringVar(&cfg.Zone, "zone", "", "DNS zone to reconcile, for example local.dev")
	flags.StringVar(&cfg.KubeContext, "kube-context", "", "Kubernetes context containing Technitium")
	flags.StringVar(&cfg.Namespace, "namespace", "technitium", "Technitium Kubernetes namespace")
	flags.StringVar(&cfg.Service, "service", "technitium", "Technitium Kubernetes Service name")
	flags.StringVar(&cfg.AdminSecret, "admin-secret", "technitium-admin", "Kubernetes Secret containing the Technitium admin password")
	flags.StringVar(&cfg.PasswordKey, "password-key", "DNS_SERVER_ADMIN_PASSWORD", "password key in the admin Secret")
	flags.StringVar(&cfg.AdminUser, "admin-user", "admin", "Technitium admin username")
	flags.IntVar(&cfg.APIPort, "api-port", 5380, "Technitium admin Service port")
	flags.StringVar(&cfg.Kubectl, "kubectl", "kubectl", "kubectl executable or absolute path")
	flags.IntVar(&cfg.TTL, "ttl", 30, "wildcard A record TTL in seconds")
	flags.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "maximum time for one reconciliation")
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
	if cfg.InterfaceName != "" && cfg.TargetIP != "" {
		return config{}, fmt.Errorf("-interface and -target-ip cannot be used together")
	}
	if cfg.TargetIP != "" {
		ip := net.ParseIP(cfg.TargetIP)
		if ip == nil || ip.To4() == nil || (!ip.IsGlobalUnicast() && !ip.IsLoopback()) {
			return config{}, fmt.Errorf("-target-ip must be a usable IPv4 address")
		}
	}
	if !validDNSName(cfg.Namespace) || strings.Contains(cfg.Namespace, ".") ||
		!validDNSName(cfg.Service) || strings.Contains(cfg.Service, ".") ||
		!validDNSName(cfg.AdminSecret) {
		return config{}, fmt.Errorf("namespace and service must be DNS labels; admin-secret must be a DNS name")
	}
	if strings.TrimSpace(cfg.PasswordKey) == "" || strings.TrimSpace(cfg.AdminUser) == "" {
		return config{}, fmt.Errorf("password-key and admin-user cannot be empty")
	}
	if cfg.APIPort < 1 || cfg.APIPort > 65535 {
		return config{}, fmt.Errorf("-api-port must be between 1 and 65535")
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
	return append([]string{"--context", cfg.KubeContext, "-n", cfg.Namespace}, args...)
}
