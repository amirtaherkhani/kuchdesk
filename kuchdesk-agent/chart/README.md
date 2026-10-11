# KuchDesk Agent Helm chart

This standalone chart deploys the Kubernetes agent as one internal cluster service. Discovery is read-only by default. It does not install an image registry, secret manager, DNS server, dashboard, or other shared services.

For the migrated home-lab release, follow the [agent namespace workflow](../../lab/migrations/agent-platform-tools/README.md): workload namespace `platform-tools`, Helm metadata `development-tools`. Ordinary unrendered upgrades would revert the move. The examples below describe a fresh installation.

Supply an image already available to the Kubernetes runtime and use an immutable tag. Set a unique cluster ID and display name for each cluster; the chart has no home-lab identity baked in. Create the bearer token Secret in the target namespace before installation; the chart reads it but does not manage its lifecycle. For example, with a token obtained through your normal secret workflow:

```bash
kubectl create namespace development-tools --dry-run=client -o yaml | kubectl apply -f -
kubectl -n development-tools create secret generic kuchdesk-agent-auth \
  --from-literal=KUCHDESK_AGENT_TOKEN="${KUCHDESK_AGENT_TOKEN}"
helm upgrade --install kuchdesk-agent ./kuchdesk-agent/chart \
  --namespace development-tools \
  --set cluster.id=development \
  --set cluster.name='Development cluster' \
  --set image.repository=REGISTRY/kuchdesk-agent \
  --set image.tag=IMMUTABLE_TAG
```

Do not put the token in Helm values or Git. If the image is loaded directly into Docker Desktop Kubernetes, use its matching repository and tag. The chart does not require a local registry. Its `appVersion` tracks the Go binary; the chart `version` tracks chart changes.

`image.digest` optionally pins the manifest by `sha256`. When supplied, the
rendered reference is `repository:tag@sha256:...`; the digest determines the
content Kubernetes pulls. The host deployment workflow resolves this digest
after a successful push and supplies it to Helm. A tag alone remains supported
for images already loaded into a local node.

Clients send `Authorization: Bearer <token>` to the internal Service. Limit which clients can reach it. `networkPolicy.enabled` is off by default; when enabling it, set `networkPolicy.ingressPodSelector` to select authorized client Pods.

The default ClusterRole allows discovery and metrics reads. CoreDNS alias writes and general resource management are disabled by default. General management requires an explicit bearer Secret and `rbac.clusterAdmin=true`, granting the agent ServiceAccount full cluster access. DNS alias management needs the bearer Secret but uses a narrow Role for `get` and `update` on the existing CoreDNS ConfigMap. Review the [agent API](../README.md) before enabling it. On Docker Desktop KIND, set `dnsManagement.enabled=true` only after backing up `kube-system/coredns` and verifying its `Corefile` contains a `.:53` block with `kubernetes` and `reload`. The agent owns only its marked block and the chart never creates the system ConfigMap.

`agent.execEnabled` is an additional opt-in switch, default `false`. It
requires `agent.managementEnabled=true`, an existing bearer Secret, and the
existing management RBAC configuration. It adds no Role or Secret. The
noninteractive Pod exec API returns bounded stdout/stderr; restrict callers to
trusted administrators because commands may read application data. Enabling
this switch requires a Helm upgrade and Pod restart; do not place commands or
tokens in chart values.

`agent.portForwardEnabled` is a separate opt-in switch, also default `false`.
It requires the same management bearer Secret and RBAC. The agent accepts only
authenticated WebSocket clients without a browser `Origin`, then opens a
short-lived loopback tunnel inside its own Pod to one running Pod port. The
session has a five-minute lifetime, a 30-second idle timeout, four concurrent
session slots, and a 64 MiB limit in each direction. It adds no public Service
port or new RBAC grant. Keep this disabled unless the clients are trusted
administrators; a tunnel can carry sensitive application traffic.

`hostBridge.enabled` is another opt-in switch, default `false`. It requires
`agent.managementEnabled=true`, the existing agent bearer Secret, an HTTPS
`hostBridge.url`, and a separate `hostBridge.tokenSecretName`. Optional
`hostBridge.caConfigMapName` supplies the host CA certificate as `ca.crt`.
The chart references only Secret and ConfigMap names; do not put either the
bridge token or Infisical Universal Auth pair in Helm values. Once the host
HTTPS service and cluster route have been approved and verified, authorized
clients can call `POST /v1/host/infisical/commands` and
`POST /v1/host/doctor` through the agent. The host bridge remains absent from
the agent API while disabled. See the [control-plane design](../../docs/host-control-plane.md).

The agent can start without `metrics.k8s.io`; CPU and memory usage remain unavailable until a compatible metrics-server is installed.

`service.targetPort` sets both the container port and the agent's HTTP listener. Use `agent.listenHost` only to change the bind host; the default listens on all interfaces. Remove any old `agent.listenAddress` override when upgrading from chart 0.5.x. Direct binary invocations also require `KUCHDESK_CLUSTER_ID` and `KUCHDESK_CLUSTER_NAME`; they no longer silently identify an unknown cluster as `default`.

When upgrading from chart 0.4.x, set `cluster.id` and `cluster.name` explicitly. Reuse the previous values for an existing installation so its identity does not change.

```bash
helm lint ./kuchdesk-agent/chart \
  --set cluster.id=example \
  --set cluster.name='Example cluster' \
  --set image.repository=example.invalid/kuchdesk-agent \
  --set image.tag=0.9.0
helm template kuchdesk-agent ./kuchdesk-agent/chart \
  --namespace development-tools \
  --set cluster.id=example \
  --set cluster.name='Example cluster' \
  --set image.repository=example.invalid/kuchdesk-agent \
  --set image.tag=0.9.0
```

`baseProject.name` defaults to `Kubedesk Platform` and populates
`KUCHDESK_BASE_PROJECT_NAME`. Optional `baseProject.id` and `baseProject.slug`
populate explicit base-project metadata, returned by authenticated
`GET /v1/platform`. These fields never choose a managed project or grant access.
Changing Helm environment values requires a rollout. Enrollment policy reload is
handled separately on the Host. The cluster's Infisical bridge refuses all write,
delete, membership and role commands regardless of cluster RBAC privileges.
