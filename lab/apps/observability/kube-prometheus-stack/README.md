# Prometheus Monitoring

Prometheus Operator, kube-state-metrics, node exporter, Alertmanager, and
Kubernetes recording/alert rules.

| Field | Value |
|---|---|
| Namespace | `observability` |
| UI | Internal services; Grafana is the primary UI |
| Ports | Prometheus `9090`, Alertmanager `9093` |
| Sources | Kubernetes and observability ServiceMonitors |
| Retention | 5 days for local development |
| Dashboards | Provisioned into Grafana |

K3s control-plane components without stable scrape Services remain disabled; bundled metrics-server is verified during later preflight.
