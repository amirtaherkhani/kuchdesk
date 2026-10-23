# Agent capabilities

See the [canonical local access guide](local-service-access.md) for current service publication, endpoint modes, read-only CLI/API/MCP visibility and bounded native recovery.

| Surface | Behavior | Boundary |
|---|---|---|
| Kubernetes Agent | Discovery, metrics, events, SSE, authenticated management; configuration supplied by Helm ENV/Secrets | Management, exec, port forwarding, and CoreDNS alias writes are separately enabled; RBAC still applies |
| Host Agent | One-shot Technitium DNS reconciliation and explicit Grafana password recovery | Uses selected Kubernetes context and existing credentials; no daemon-side secret manager |
| Host Bridge | HTTPS Doctor diagnostics, read-only publication status and opt-in native TCP recovery | Separate bearer/TLS; UDP recovery unsupported; no application-secret operations |
| MCP | Doctor, read-only service-access status, canonical YAML read/edit, typed plan validation and opt-in deployment jobs | Deployment requires an enabled profile, preflight, and exact release/namespace confirmation |
| Local charts | Direct values YAML, with ignored local credential overlays and namespaced Secrets, including shared databases/brokers | Values changes do not rotate credentials in existing databases or running services |

The Agent Helm chart defaults to read-only discovery. CoreDNS writes and general management require explicit enablement and Kubernetes permissions. The Host DNS job remains one-shot. MCP jobs are bounded in process memory; a restart loses job history and cancellation cannot undo an accepted mutation.

Use `lab/site.json` for generated DNS and certificate manifests. Set Grafana hostname and TLS references directly in its values YAML. Review both before a domain change. Runtime readiness and effective RBAC require live checks after deployment; source validation alone does not establish them.
