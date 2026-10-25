package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/localruntime"
)

type commandRunner func(context.Context, string, ...string) ([]byte, error)

func nativeCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Resolve the VM address and Lima's loopback API listener on every launchd run.
// Refuse stale or unrelated kubeconfig endpoints before modifying DNS.
func resolveColimaTarget(ctx context.Context, cfg config, run commandRunner) (config, error) {
	profile, err := localruntime.Profile(cfg.KubeContext)
	if err != nil {
		return cfg, err
	}
	data, err := run(ctx, "colima", "--profile", profile, "status", "--json")
	if err != nil {
		return cfg, errors.New("selected Colima profile is unavailable")
	}
	var status struct {
		Runtime string `json:"runtime"`
		Address string `json:"ip_address"`
	}
	if json.Unmarshal(data, &status) != nil || status.Runtime != "containerd" {
		return cfg, errors.New("selected Colima profile is not running containerd")
	}
	ip := net.ParseIP(status.Address)
	if ip == nil || ip.To4() == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() {
		return cfg, errors.New("selected Colima profile has no usable ingress IPv4 address")
	}
	data, err = run(ctx, cfg.Kubectl, "--context", cfg.KubeContext, "config", "view", "--minify", "-o", "jsonpath={.clusters[0].cluster.server}")
	if err != nil {
		return cfg, errors.New("selected Kubernetes API endpoint is unavailable")
	}
	endpoint, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil || endpoint.Scheme != "https" || (endpoint.Hostname() != ip.String() && endpoint.Hostname() != "127.0.0.1") || endpoint.User != nil || (endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return cfg, errors.New("selected Kubernetes API does not match the Colima VM")
	}
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil || port < 1 || port > 65535 {
		return cfg, errors.New("selected Kubernetes API port is invalid")
	}
	cfg.TargetIP = ip.String()
	cfg.KubeAPIServer = fmt.Sprintf("https://127.0.0.1:%d", port)
	return cfg, nil
}
