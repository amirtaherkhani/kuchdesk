package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Config struct {
	BaseProjectName      string
	BaseProjectID        string
	BaseProjectSlug      string
	ListenAddress        string
	ClusterID            string
	ClusterName          string
	ClusterDomain        string
	Kubeconfig           string
	KubeContext          string
	BearerToken          string
	DNSManagementEnabled bool
	ManagementEnabled    bool
	ExecEnabled          bool
	PortForwardEnabled   bool
	HostBridgeEnabled    bool
	HostBridgeURL        string
	HostBridgeToken      string
	HostBridgeCAFile     string
	CoreDNSNamespace     string
	CoreDNSConfigMap     string
	CoreDNSCorefileKey   string
	MetricsInterval      time.Duration
	RefreshDebounce      time.Duration
	SSEHeartbeat         time.Duration
	SSEHistory           int
	EventLimit           int
}

func Load() (Config, error) {
	for _, legacy := range []string{"KUCHDESK_COREDNS_CUSTOM_CONFIGMAP", "KUCHDESK_COREDNS_OVERRIDE_KEY"} {
		if _, present := os.LookupEnv(legacy); present {
			return Config{}, fmt.Errorf("%s is no longer supported; use KUCHDESK_COREDNS_CONFIGMAP and KUCHDESK_COREDNS_COREFILE_KEY", legacy)
		}
	}
	cfg := Config{
		BaseProjectName:    os.Getenv("KUCHDESK_BASE_PROJECT_NAME"),
		BaseProjectID:      os.Getenv("KUCHDESK_BASE_PROJECT_ID"),
		BaseProjectSlug:    os.Getenv("KUCHDESK_BASE_PROJECT_SLUG"),
		ListenAddress:      envOrDefault("KUCHDESK_AGENT_LISTEN_ADDRESS", ":8080"),
		ClusterID:          strings.TrimSpace(os.Getenv("KUCHDESK_CLUSTER_ID")),
		ClusterName:        strings.TrimSpace(os.Getenv("KUCHDESK_CLUSTER_NAME")),
		ClusterDomain:      strings.Trim(envOrDefault("KUCHDESK_CLUSTER_DOMAIN", "cluster.local"), "."),
		Kubeconfig:         os.Getenv("KUBECONFIG"),
		KubeContext:        strings.TrimSpace(os.Getenv("KUCHDESK_KUBE_CONTEXT")),
		BearerToken:        strings.TrimSpace(os.Getenv("KUCHDESK_AGENT_TOKEN")),
		HostBridgeURL:      strings.TrimSpace(os.Getenv("KUCHDESK_HOST_BRIDGE_URL")),
		HostBridgeToken:    strings.TrimSpace(os.Getenv("KUCHDESK_HOST_BRIDGE_TOKEN")),
		HostBridgeCAFile:   strings.TrimSpace(os.Getenv("KUCHDESK_HOST_BRIDGE_CA_FILE")),
		CoreDNSNamespace:   envOrDefault("KUCHDESK_COREDNS_NAMESPACE", "kube-system"),
		CoreDNSConfigMap:   envOrDefault("KUCHDESK_COREDNS_CONFIGMAP", "coredns"),
		CoreDNSCorefileKey: envOrDefault("KUCHDESK_COREDNS_COREFILE_KEY", "Corefile"),
		MetricsInterval:    10 * time.Second,
		RefreshDebounce:    250 * time.Millisecond,
		SSEHeartbeat:       15 * time.Second,
		SSEHistory:         256,
		EventLimit:         100,
	}

	if cfg.BaseProjectName == "" {
		cfg.BaseProjectName = "Kubedesk Platform"
	}
	cfg.BaseProjectName = strings.TrimSpace(cfg.BaseProjectName)
	if cfg.BaseProjectName == "" || !utf8.ValidString(cfg.BaseProjectName) || utf8.RuneCountInString(cfg.BaseProjectName) > 128 || strings.IndexFunc(cfg.BaseProjectName, unicode.IsControl) >= 0 {
		return Config{}, errors.New("invalid KUCHDESK_BASE_PROJECT_NAME")
	}
	for _, v := range []string{cfg.BaseProjectID, cfg.BaseProjectSlug} {
		if strings.ContainsAny(v, "/\\?# \t\n\r") || len(v) > 128 {
			return Config{}, errors.New("invalid base project selector")
		}
	}

	var err error
	if cfg.DNSManagementEnabled, err = boolEnv("KUCHDESK_DNS_MANAGEMENT_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.ManagementEnabled, err = boolEnv("KUCHDESK_MANAGEMENT_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.ExecEnabled, err = boolEnv("KUCHDESK_EXEC_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.PortForwardEnabled, err = boolEnv("KUCHDESK_PORT_FORWARD_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.HostBridgeEnabled, err = boolEnv("KUCHDESK_HOST_BRIDGE_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.MetricsInterval, err = durationEnv("KUCHDESK_METRICS_INTERVAL", cfg.MetricsInterval); err != nil {
		return Config{}, err
	}
	if cfg.RefreshDebounce, err = durationEnv("KUCHDESK_REFRESH_DEBOUNCE", cfg.RefreshDebounce); err != nil {
		return Config{}, err
	}
	if cfg.SSEHeartbeat, err = durationEnv("KUCHDESK_SSE_HEARTBEAT", cfg.SSEHeartbeat); err != nil {
		return Config{}, err
	}
	if cfg.SSEHistory, err = intEnv("KUCHDESK_SSE_HISTORY", cfg.SSEHistory, 8, 4096); err != nil {
		return Config{}, err
	}
	if cfg.EventLimit, err = intEnv("KUCHDESK_EVENT_LIMIT", cfg.EventLimit, 0, 1000); err != nil {
		return Config{}, err
	}
	if cfg.ClusterID == "" {
		return Config{}, errors.New("KUCHDESK_CLUSTER_ID is required")
	}
	if cfg.ClusterName == "" {
		return Config{}, errors.New("KUCHDESK_CLUSTER_NAME is required")
	}
	if cfg.ClusterDomain == "" {
		return Config{}, errors.New("KUCHDESK_CLUSTER_DOMAIN cannot be empty")
	}
	if cfg.BearerToken == "" {
		return Config{}, errors.New("KUCHDESK_AGENT_TOKEN is required")
	}
	if cfg.DNSManagementEnabled {
		if cfg.CoreDNSNamespace == "" || cfg.CoreDNSConfigMap == "" || cfg.CoreDNSCorefileKey == "" {
			return Config{}, errors.New("CoreDNS namespace, ConfigMap, and Corefile key cannot be empty")
		}
	}
	if cfg.ExecEnabled && !cfg.ManagementEnabled {
		return Config{}, errors.New("KUCHDESK_MANAGEMENT_ENABLED is required when exec is enabled")
	}
	if cfg.PortForwardEnabled && !cfg.ManagementEnabled {
		return Config{}, errors.New("KUCHDESK_MANAGEMENT_ENABLED is required when port-forward is enabled")
	}
	if cfg.HostBridgeEnabled && (cfg.BearerToken == "" || cfg.HostBridgeURL == "" || len(cfg.HostBridgeToken) < 32) {
		return Config{}, errors.New("host bridge requires agent bearer, HTTPS URL, and a dedicated bridge bearer")
	}
	return cfg, nil
}

func RESTConfig(cfg Config) (*rest.Config, error) {
	var (
		restConfig *rest.Config
		err        error
	)
	if cfg.Kubeconfig == "" && cfg.KubeContext == "" {
		restConfig, err = rest.InClusterConfig()
		if err != nil && !errors.Is(err, rest.ErrNotInCluster) {
			return nil, fmt.Errorf("load in-cluster Kubernetes client configuration: %w", err)
		}
	}
	if restConfig == nil {
		if !regexp.MustCompile(`^colima(?:-[a-z0-9][a-z0-9-]{0,62})?$`).MatchString(cfg.KubeContext) {
			return nil, errors.New("host execution requires an explicit Colima context; current-context fallback is disabled")
		}
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		if cfg.Kubeconfig != "" {
			rules.Precedence = filepath.SplitList(cfg.Kubeconfig)
		}
		restConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			rules,
			&clientcmd.ConfigOverrides{CurrentContext: cfg.KubeContext},
		).ClientConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes client configuration: %w", err)
	}

	restConfig.UserAgent = "kuchdesk-agent/0.6.0"
	restConfig.QPS = 30
	restConfig.Burst = 60
	restConfig.Timeout = 30 * time.Second
	return restConfig, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}

func intEnv(key string, fallback, minimum, maximum int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minimum, maximum)
	}
	return parsed, nil
}
