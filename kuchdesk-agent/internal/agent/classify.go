package agent

import (
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	CategoryWebApplications  = "web-applications"
	CategoryDatabasesStorage = "databases-storage"
	CategoryObservability    = "observability-metrics"
	CategoryAutomation       = "automation-workflows"
	CategoryDeployments      = "deployments"
	CategoryAIServices       = "ai-services"
	CategoryMessagingEvents  = "messaging-events"
	CategoryDeveloperTools   = "developer-tools"
	CategoryPlatformSecurity = "platform-security"
	CategoryOther            = "other"
)

var categoryAliases = map[string]string{
	"web":                   CategoryWebApplications,
	"web-app":               CategoryWebApplications,
	"web-applications":      CategoryWebApplications,
	"database":              CategoryDatabasesStorage,
	"databases":             CategoryDatabasesStorage,
	"storage":               CategoryDatabasesStorage,
	"databases-storage":     CategoryDatabasesStorage,
	"observability":         CategoryObservability,
	"metrics":               CategoryObservability,
	"observability-metrics": CategoryObservability,
	"automation":            CategoryAutomation,
	"workflow":              CategoryAutomation,
	"automation-workflows":  CategoryAutomation,
	"deployment":            CategoryDeployments,
	"deployments":           CategoryDeployments,
	"testing":               CategoryDeployments,
	"ai":                    CategoryAIServices,
	"mcp":                   CategoryAIServices,
	"ai-mcp":                CategoryAIServices,
	"ai-services":           CategoryAIServices,
	"messaging":             CategoryMessagingEvents,
	"events":                CategoryMessagingEvents,
	"messaging-events":      CategoryMessagingEvents,
	"developer":             CategoryDeveloperTools,
	"developer-tools":       CategoryDeveloperTools,
	"platform":              CategoryPlatformSecurity,
	"security":              CategoryPlatformSecurity,
	"platform-security":     CategoryPlatformSecurity,
	"other":                 CategoryOther,
}

type classificationRule struct {
	category string
	terms    []string
}

var classificationRules = []classificationRule{
	{
		category: CategoryDatabasesStorage,
		terms: []string{
			"postgres", "mysql", "mariadb", "mongodb", "mongo", "redis",
			"valkey", "minio", "object-storage", "clickhouse", "cassandra",
			"elasticsearch", "opensearch", "database", "storage", "pgadmin",
		},
	},
	{
		category: CategoryObservability,
		terms: []string{
			"prometheus", "grafana", "loki", "tempo", "jaeger", "alloy",
			"opentelemetry", "otel", "victoria-metrics", "metrics-server",
			"kube-state-metrics", "observability", "monitoring", "caretta", "radar",
		},
	},
	{
		category: CategoryAutomation,
		terms: []string{
			"airflow", "argo-workflows", "tekton",
			"windmill", "workflow", "automation",
		},
	},
	{
		category: CategoryDeployments,
		terms: []string{
			"argocd", "argo-cd", "flux", "k6", "sonarqube", "jenkins",
			"gitlab-runner", "deployment", "load-test", "testing",
		},
	},
	{
		category: CategoryAIServices,
		terms: []string{
			"mcp", "ollama", "vllm", "litellm", "open-webui", "inference",
			"model-server", "ai-service", "langfuse",
		},
	},
	{
		category: CategoryMessagingEvents,
		terms: []string{
			"kafka", "nats", "rabbitmq", "redpanda", "pulsar", "schema-registry",
			"message-broker", "event-stream",
		},
	},
	{
		category: CategoryPlatformSecurity,
		terms: []string{
			"vault", "cert-manager", "external-secrets", "keycloak",
			"dex", "oauth", "traefik", "ingress-nginx", "cilium", "security",
		},
	},
	{
		category: CategoryDeveloperTools,
		terms: []string{
			"mailpit", "mailhog", "grpcui", "swagger", "backstage", "developer-tool",
		},
	},
}

func ClassifyService(
	name string,
	labels map[string]string,
	annotations map[string]string,
	ports []corev1.ServicePort,
	hasIngress bool,
) string {
	for _, metadata := range []map[string]string{annotations, labels} {
		for _, key := range []string{
			"app.kuchdesk.io/category",
			"kuchdesk.io/category",
			"homepage.kuchdesk.io/category",
		} {
			if category, ok := normalizeCategory(metadata[key]); ok {
				return category
			}
		}
	}

	parts := []string{name}
	for _, key := range []string{
		"app.kubernetes.io/name",
		"app.kubernetes.io/component",
		"app.kubernetes.io/part-of",
		"app",
		"component",
	} {
		parts = append(parts, labels[key])
	}
	for _, port := range ports {
		parts = append(parts, port.Name, strconv.Itoa(int(port.Port)))
	}
	haystack := strings.ToLower(strings.Join(parts, " "))
	for _, rule := range classificationRules {
		for _, term := range rule.terms {
			if strings.Contains(haystack, term) {
				return rule.category
			}
		}
	}

	if hasIngress || hasHTTPPort(ports) {
		return CategoryWebApplications
	}
	return CategoryOther
}

func normalizeCategory(value string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("&", "-", "_", "-", " ", "-").Replace(normalized)
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}
	normalized = strings.Trim(normalized, "-")
	category, ok := categoryAliases[normalized]
	return category, ok
}

func hasHTTPPort(ports []corev1.ServicePort) bool {
	for _, port := range ports {
		name := strings.ToLower(port.Name)
		if strings.Contains(name, "http") || port.Port == 80 || port.Port == 443 ||
			port.Port == 3000 || port.Port == 8080 || port.Port == 8443 {
			return true
		}
	}
	return false
}
