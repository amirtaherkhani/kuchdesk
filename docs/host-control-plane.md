# Host control plane

See the [canonical local access guide](local-service-access.md) for current service publication, endpoint modes, read-only CLI/API/MCP visibility and bounded native recovery.

The optional Host Bridge is an HTTPS service for bounded Doctor diagnostics and read-only catalog/publication status. The Kubernetes Agent forwards authenticated `POST /v1/host/doctor` requests to the Host Bridge over TLS. The Agent uses its own bearer for client requests and a separate bridge bearer for this hop. The Host Bridge exposes `POST /v1/doctor`, authenticated `GET /v1/service-access` and `/healthz`; it has no application-secret command API.

The bridge bearer is supplied through `KUCHDESK_HOST_BRIDGE_TOKEN` on the Host and an existing Kubernetes Secret reference in the Agent chart. Its value is never placed in tracked YAML. The bridge does not start unless explicitly enabled. Kubernetes discovery and the one-shot Technitium DNS Host Agent remain independent.

Optional `forwardingRecovery` runs under this existing KeepAlive Bridge, independently of read-only requests. It reuses native Lima TCP operations with private ownership receipts and bounded retries. UDP recovery remains unsupported; the scheduled DNS Agent keeps its one-shot lifecycle.
