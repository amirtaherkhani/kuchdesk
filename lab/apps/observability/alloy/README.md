# Grafana Alloy

KuchDesk's telemetry collector runs as a DaemonSet in `observability`. The
internal Service `alloy` keeps OTLP gRPC `4317` and OTLP HTTP `4318` for Pods;
the separately Helm-owned `alloy-publication` Service publishes only those
receivers on TCP20007 and TCP20006. The Alloy UI is a separate HTTPS panel.

## Signal routes

* OTLP/HTTP traces, metrics and logs: append `/v1/traces`, `/v1/metrics`, or
  `/v1/logs` to the base endpoint.
* OTLP/gRPC: trace acceptance is verified; metrics/logs and non-Mac gRPC
  clients remain unverified.
* Accepted OTLP metrics are forwarded to the existing Prometheus remote-write
  receiver. Logs go to Loki with bounded service, namespace, environment and
  client-mode labels. Traces go to Tempo.
* Alloy reads Kubernetes Pod stdout through the Kubernetes API. It does not
  tail Mac files or ordinary container logs and is not given containerd-socket
  access. Those applications must send OTLP logs explicitly.

## Client endpoints and current evidence

| Client | OTLP/HTTP | OTLP/gRPC | Evidence |
| --- | --- | --- | --- |
| Mac | `http://192.168.64.15:20006` | `192.168.64.15:20007` | HTTP traces/metrics/logs accepted; gRPC trace accepted and queried. Mac loopback listeners on `127.0.0.1:20006/20007` exist, but protocol requests time out. |
| Ordinary nerdctl bridge container | `http://192.168.64.15:20006` | `192.168.64.15:20007` | HTTP traces/metrics/logs accepted. gRPC not tested. |
| Kubernetes Pod | `http://alloy.observability.svc.cluster.local:4318` | `alloy.observability.svc.cluster.local:4317` | HTTP traces/metrics/logs accepted. gRPC not tested. |

The published route is plaintext without OTLP authentication and is intended
for local development. The LoadBalancer Service has explicit source CIDRs; its
VM-side exposure is distinct from the Mac loopback listener. An independent
LAN test was unavailable, so LAN isolation is not claimed. The loopback timeout
source remains unresolved; use the verified VM address until that route is
diagnosed. Do not widen source CIDRs based only on a guessed forwarded source.

Prometheus uses a 10Gi `local-path` PVC and three-day retention. The previous
`emptyDir` history was not migrated; a later Prometheus Pod-replacement
durability check remains pending. See the
[canonical local access guide](../../../../docs/local-service-access.md) and
the [sanitized acceptance report](../../../../.kuchdesk/diagnostics/observability-three-mode-acceptance-2026-10-19.md).
