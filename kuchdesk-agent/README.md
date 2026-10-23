# KuchDesk Agent

KuchDesk Agent is a small Go service that runs once per Kubernetes cluster. It
watches the Kubernetes API, reads the resource Metrics API, builds a
cluster snapshot, and streams updates to authorized clients over
Server-Sent Events (SSE). Discovery is read-only. An optional, disabled-by-
default CoreDNS module can manage exact internal aliases for Kubernetes
Services.

The agent talks to the Kubernetes API server, not directly to every node. This
is the safest and most portable approach: `client-go` maintains efficient
list/watch caches while metrics-server already aggregates CPU and memory from
each kubelet.

## Package choices

- [`k8s.io/client-go`](https://github.com/kubernetes/client-go) provides the
  official typed Kubernetes client, discovery client, shared informers, listers,
  watch reconnection, and local caches.
- [`k8s.io/metrics`](https://github.com/kubernetes/metrics) provides the typed
  `metrics.k8s.io/v1beta1` client used for node and pod CPU/memory snapshots.
- [`github.com/coredns/caddy/caddyfile`](https://github.com/coredns/caddy)
  provides CoreDNS's maintained Corefile lexer and parser. It validates the
  generated rewrite directives and Corefile syntax before the Kubernetes API is updated.
- Go's [`net/http`](https://pkg.go.dev/net/http) provides the SSE server,
  streaming flush support, connection cancellation, and production HTTP
  timeouts without another runtime dependency.

The module pins the Kubernetes packages to `v0.36.3`, matching Kubernetes
`v1.36.x`. Keep the `client-go`, API machinery, API, and metrics module minor
versions aligned when upgrading.

## Collected data

- Nodes, readiness conditions, roles, addresses, versions, capacity, pod
  allocation, CPU, memory, and requested ephemeral storage
- Pods, containers, readiness, restart counts, owners, IPs, images, and
  CPU/memory
- Deployments, StatefulSets, and DaemonSets with desired/ready counts, selectors,
  and the latest matching Pod creation time as `lastDeployedAt`
- Services, cluster DNS names, ClusterIPs, ports, matching Pods, EndpointSlice
  readiness, related workload references, ingress URLs, uptime, and intelligent
  category assignment
- Ingress routes and TLS hosts
- PersistentVolumes and PersistentVolumeClaims
- CoreDNS service name, DNS service IP, ports, endpoint readiness, cluster
  domain, and search path
- Recent Kubernetes events for the notification surface

Storage percentage is explicitly
`ephemeralStorageRequestPercent`: Kubernetes Metrics API does not publish node
filesystem usage. The agent intentionally does not request broad `nodes/proxy`
permission to scrape kubelet Summary APIs.

## HTTP and SSE contract

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Process liveness |
| `GET /readyz` | Informer cache readiness |
| `GET /v1/snapshot` | Current `kuchdesk.io/v1alpha1` JSON snapshot |
| `GET /v1/events` | Reconnecting SSE stream |
| `GET /v1/dns/config` | CoreDNS management state and service aliases |
| `PUT /v1/dns/config` | Validate, preview, or replace managed service aliases |
| `GET /v1/manage/capabilities?namespace=apps` | Served resources and subresources, their verbs, point-in-time authorization checks, and Metrics API availability |
| `GET/POST/PUT/PATCH/DELETE /v1/manage/resources/{group}/{version}/{resource}` | Bounded Kubernetes resource operations; use `core` for the core API group |
| `GET /v1/manage/pods/{namespace}/{name}/logs` | Up to 1 MiB and 1,000 tail lines of pod logs |
| `POST /v1/manage/pods/{namespace}/{name}/exec` | Opt-in, noninteractive Pod command with bounded output |
| `GET /v1/manage/pods/{namespace}/{name}/port-forward?port=PORT` | Opt-in WebSocket binary tunnel to one Pod TCP port |
| `GET /v1/manage/batch/jobs/{namespace}/{name}` | Job identity, counts, conditions, and completion time without Pod template |
| `POST /v1/manage/batch/cronjobs/{namespace}/{name}/run` | Create one Job from the confirmed CronJob template; supports `dryRun=true` |
| `DELETE /v1/manage/batch/jobs/{namespace}/{name}` | Delete one confirmed Job and its Pods with Kubernetes preconditions; supports `dryRun=true` |
| `GET /v1/manage/events/{namespace}` | Up to 100 namespace events |
| `GET /v1/manage/workloads/{kind}/{namespace}/{name}/{action}` | Deployment/StatefulSet status or scale; DaemonSet status |
| `POST /v1/manage/workloads/{kind}/{namespace}/{name}/{action}` | Deployment/StatefulSet scale, restart, status; DaemonSet restart/status |
| `POST /v1/manage/workloads/{kind}/{namespace}/{name}/{action}/jobs` | Start an in-process scale or restart job after the same exact confirmation and version checks |
| `GET /v1/manage/jobs/{id}` | Check a job's state and Kubernetes API response status without storing a workload response body |
| `DELETE /v1/manage/jobs/{id}` | Request cooperative cancellation of a queued or running job |

Management is disabled by default with `KUCHDESK_MANAGEMENT_ENABLED=false`.
It requires a non-empty bearer token and an intentionally granted Kubernetes
ServiceAccount role. The Helm chart requires `rbac.clusterAdmin=true` when
management is enabled. Clients must authenticate directly to the internal
agent API; avoid caching management responses.

Resource operations use discovery for served resource scope. `GET` supports
`name` or a paginated list (`continue`), and a bounded newline-delimited watch
with `watch=true&resourceVersion=...`. Namespace-scoped writes require
`namespace`; `PUT` requires `metadata.resourceVersion`; merge `PATCH` and
server-side apply require `If-Match` with the resource version. Apply also
requires `fieldManager`. Write operations require exact
`X-KuchDesk-Confirm: namespace/name`; `DELETE` additionally requires
`If-Match-UID` and `If-Match` with the current resource version. Pass
`dryRun=true` to ask the API server to validate without
persisting a write. Scale accepts JSON `replicas` (0–100) and
`resourceVersion`; restart requires `If-Match` and patches only the workload's
pod-template restart annotation. The API server still enforces its own RBAC.
Capabilities include `allowedVerbs` from a complete SelfSubjectRulesReview
for the agent ServiceAccount. Incomplete or unavailable rules fall back to
bounded per-verb SelfSubjectAccessReviews. Pass a concrete `namespace` to evaluate namespaced
resources; without one, their `allowedVerbs` are empty. Authorization results
are informational snapshots, not grants, and Kubernetes checks every actual
operation again. `GET` workload status returns the typed workload object;
`GET` scale returns the typed Scale object with its resource version.

Asynchronous workload jobs are available only when management is enabled. Four
can run concurrently; 64 records are retained in memory. Jobs for the same
workload serialize, and each has a 30-second deadline. The returned
`succeeded` state means the Kubernetes API accepted the scale or restart
request; it does **not** mean the rollout became healthy. Query the workload
status and Pods separately. Cancellation cannot undo a mutation already
accepted by Kubernetes. Agent restart cancels in-flight work and loses job
history; this feature is not a durable task service.

Pod exec requires `KUCHDESK_EXEC_ENABLED=true` in addition to management mode
and the bearer token. Send JSON such as
`{"container":"main","command":["/bin/true"]}` with exact
`X-KuchDesk-Confirm: pods/NAMESPACE/NAME/exec`, `If-Match-UID` and `If-Match`
(Pod resource version) headers. The target Pod and container must be running.
The API accepts no stdin or TTY, runs at most four commands concurrently, gives
each command 30 seconds, and caps each output stream at 256 KiB. It returns
`stdout`, `stderr`, and `exitCode`; nonzero command exits still return HTTP 200.
The Pod is checked before starting, but Kubernetes exec addresses a Pod by
name, so a replacement between the check and stream setup cannot be ruled out.
Do not use this endpoint for destructive commands against rapidly recreated
Pods. Output may contain application secrets and is never written to audit
logs; restrict API clients and handle returned output accordingly.

Pod port-forward requires `KUCHDESK_PORT_FORWARD_ENABLED=true`, management
mode, and bearer authentication. Use a WebSocket client without an `Origin`
header. Supply exact `X-KuchDesk-Confirm:
pods/NAMESPACE/NAME/port-forward/PORT`, `If-Match-UID`, and `If-Match`
headers from a fresh Pod read. The Pod must be running. Only binary WebSocket
frames are accepted; one connection forwards to one TCP port. The agent opens
the Kubernetes tunnel on its own `127.0.0.1` and does not expose a new Service
port. Sessions are limited to four, five minutes total, 30 seconds idle in
either direction, 64 KiB per WebSocket frame, and 64 MiB per direction. A
client disconnect closes the tunnel. Kubernetes addresses the Pod by name
during tunnel setup, so a replacement after the identity check remains a
small race; avoid rapidly recreated Pods. Tunnel bytes are not audit-logged,
but may contain secrets and must be handled by trusted clients.

Raw Secret resources are excluded from generic discovery and operations. Logs,
events, ConfigMaps, and other resources can contain sensitive content; limit
agent API client access and avoid saving these responses. Audit logs
record action metadata and request IDs, not request or response bodies.
Manual CronJob runs require `X-KuchDesk-Confirm:
cronjobs/NAMESPACE/NAME/run`, `If-Match-UID`, and `If-Match` headers from a
fresh CronJob read. The new Job copies the CronJob's Job template, uses a
generated name, and returns metadata and status only. Deleting a Job requires
`X-KuchDesk-Confirm: jobs/NAMESPACE/NAME/delete` and the Job's UID and
resourceVersion in the same headers. Kubernetes checks those preconditions
and uses foreground deletion, which also removes the Job's Pods. Both writes
support `dryRun=true`; a Job create only confirms API acceptance, so inspect
its status and Pods for eventual success. A CronJob changed after the agent's
read but before Job creation is not atomically prevented by Kubernetes; the
request uses the template fetched by the agent.

Attach, Helm operations, and generic subresource writes are not
implemented; each needs separate streaming and authorization design.

The stream sends an initial snapshot, debounced resource-change snapshots,
metrics snapshots, and heartbeat comments. It supports the standard
`Last-Event-ID` header and replays a bounded in-memory history. When the client
falls behind that history, the agent sends only the newest full snapshot so
stale replay cannot roll a client backward:

```text
retry: 3000

id: 42
event: snapshot
data: {"id":42,"name":"snapshot","clusterId":"homelab","sentAt":"...","data":{...}}
```

If `KUCHDESK_AGENT_TOKEN` is set, snapshot and stream requests require
`Authorization: Bearer <token>`. Keep the token in a trusted client process;
native browser `EventSource` cannot set an Authorization header.

## CoreDNS service aliases

On K3s, CoreDNS reads `kube-system/coredns`'s `Corefile` key. The Agent accepts a single standard `.:53` block with `kubernetes` and `reload`; verify that shape before enabling writes on the new cluster.
When explicitly enabled, the agent inserts a marked block into the existing
`.:53` server block. It preserves all other Corefile content and refuses an
unsupported layout, incomplete markers, or a rewrite outside its managed
block. The chart does not create or own the system ConfigMap. Each alias is an
exact rewrite to an existing Service:

```text
rewrite stop name exact grafana.home.arpa grafana.monitoring.svc.cluster.local
```

Example request:

```json
{
  "resourceVersion": "18",
  "aliases": [
    {
      "hostname": "grafana.home.arpa",
      "service": "grafana",
      "namespace": "monitoring"
    }
  ],
  "dryRun": false
}
```

First `GET /v1/dns/config`, then pass its opaque `resourceVersion` to `PUT`.
This provides optimistic concurrency, so two administrators cannot silently
overwrite each other's changes. Set `dryRun: true` to validate and preview the
managed directives without updating Kubernetes.

The agent validates DNS names, confirms every target Service exists, rejects
duplicate aliases and aliases that shadow native `*.svc.<cluster-domain>`
records, refuses to replace unrecognized content in its block, and publishes a
`dns.config.changed` SSE event after a successful write.

Before enabling writes, save the current `Corefile` and verify the CoreDNS
Deployment is healthy. Start with `dryRun: true`. To roll back an alias change,
GET the current resource version and PUT an empty `aliases` array; this removes
only the managed block. If CoreDNS does not recover, restore the saved Corefile
through a reviewed Kubernetes change. CoreDNS's `reload` directive applies a
valid update without restarting its Pods.

This feature configures internal cluster DNS only. Public DNS and Ingress host
records should be managed with an authoritative DNS provider, typically
through `external-dns`.

## Configuration

| Environment variable | Default |
| --- | --- |
| `KUCHDESK_AGENT_LISTEN_ADDRESS` | `:8080` |
| `KUCHDESK_CLUSTER_ID` | required; unique, stable cluster identity |
| `KUCHDESK_CLUSTER_NAME` | required; human-readable display name |
| `KUCHDESK_CLUSTER_DOMAIN` | `cluster.local` |
| `KUCHDESK_AGENT_TOKEN` | required; protects every API endpoint except health/readiness probes |
| `KUCHDESK_KUBE_CONTEXT` | required explicit `colima` or `colima-<profile>` context for host execution; unset in-cluster |
| `KUCHDESK_DNS_MANAGEMENT_ENABLED` | `false` |
| `KUCHDESK_COREDNS_NAMESPACE` | `kube-system` |
| `KUCHDESK_COREDNS_CONFIGMAP` | `coredns` |
| `KUCHDESK_COREDNS_COREFILE_KEY` | `Corefile` |
| `KUCHDESK_METRICS_INTERVAL` | `10s` |
| `KUCHDESK_REFRESH_DEBOUNCE` | `250ms` |
| `KUCHDESK_SSE_HEARTBEAT` | `15s` |
| `KUCHDESK_SSE_HISTORY` | `256` |
| `KUCHDESK_EVENT_LIMIT` | `100` |
| `KUBECONFIG` | empty; in-cluster credentials first, then standard local kubeconfig |

Inside Kubernetes, the agent uses its mounted ServiceAccount unless a
`KUBECONFIG` or `KUCHDESK_KUBE_CONTEXT` override is supplied. On a developer
Mac, it uses the standard kubeconfig search path and supports the
platform's multi-file `KUBECONFIG` path list. Host execution requires an explicit
`KUCHDESK_KUBE_CONTEXT=colima` or `colima-<profile>`; current-context fallback is disabled. The Helm chart leaves both
local overrides unset and continues to use the in-cluster ServiceAccount.

DNS management requires a non-empty `KUCHDESK_AGENT_TOKEN`; the agent refuses
to start with unauthenticated DNS writes. The Helm chart adds a namespaced Role
that can `get` and `update` only the configured existing CoreDNS ConfigMap. It
does not grant ConfigMap creation, Secret access, or node proxy access. Older
`KUCHDESK_COREDNS_CUSTOM_CONFIGMAP` and `KUCHDESK_COREDNS_OVERRIDE_KEY` settings
must be replaced when upgrading.

For local development:

```bash
KUCHDESK_CLUSTER_ID=local \
KUCHDESK_CLUSTER_NAME='Local cluster' \
KUCHDESK_AGENT_TOKEN="$LOCAL_AGENT_TOKEN" \
KUCHDESK_KUBE_CONTEXT=colima-kuchdesk-fresh \
go run ./cmd/kuchdesk-agent
```

Run the complete agent verification suite with race detection and static
analysis:

```bash
go test -race ./...
go vet ./...
```

## Canonical YAML and Colima deployment

Host/MCP reads and edits tracked service/chart YAML plus ignored mode-0600
`values.local.yaml`. Helm consumes those files and supplies the Agent's Pod
configuration through ENV and Kubernetes Secrets. The Agent never mounts Mac
configuration files and has no direct YAML read/edit endpoints. Changes require
an explicit Helm upgrade and Pod rollout; password rotation also requires the
service's credential procedure and corresponding client updates.

The chart creates the Agent bearer Secret from local `auth.token` when
`auth.existingSecret` is empty. Existing Secret references remain supported.
Every API endpoint requires bearer authentication; `/healthz` and `/readyz`
remain available to Kubernetes probes. The optional HTTPS Host Bridge retains
its separately authenticated Doctor route independently of Kubernetes management.
It works with `agent.managementEnabled=false` and `rbac.clusterAdmin=false`. Set its private bearer through
`hostBridge.token` and public trust certificate through `hostBridge.caPEM` in the
ignored Agent overlay; Helm creates matching Secret and CA ConfigMap references.
Existing `tokenSecretName` and `caConfigMapName` references remain supported as
alternatives. The Host listener starts from mode-0600
`host-agent/bridge-values.local.yaml` using `kuchdesk-host-bridge -values <path>`;
its `token` must match the Agent overlay and its certificate must cover the
configured bridge URL.

Build the OCI image from the repository root with `kuchdesk-agent/Dockerfile`
using Colima's containerd build tooling. Record the source commit, dirty build
input identity when applicable, platform, and resulting image digest. The chart
accepts `git-<12-hex>` or `git-<12-hex>-dirty-<12-hex-input-hash>` tags and an optional
verified digest. Deliver the complete image into the selected Colima profile's
Kubernetes-visible `k8s.io` containerd namespace before deployment. A tag in
another image store does not establish that k3s can run it. The root
`.dockerignore` excludes private local YAML. This chart does not register MCP.
