# Local service access

KuchDesk uses Colima, containerd, k3s and guest `nerdctl`. This Mac's installation
is profile `kuchdesk-fresh`, context `colima-kuchdesk-fresh`, VM `192.168.64.15`.
Verify the selected profile/address for another installation. Docker Desktop
and KIND describe historical installations, not fresh-install prerequisites.
Observed versions on 2026-10-19 were Colima 0.10.3, Lima 2.2.1, containerd 2.3.1
and k3s v1.35.0+k3s1; they are evidence, not permanent version restrictions.

## Existing-profile ownership migration

Before activating upgraded Host components on a legacy profile, review its
ownership using `python3 lab/scripts/environment_binding.py status`. A legacy
checkout marker requires explicit migration; do not recreate the VM or reset
the forwarding retry budget. After migration approval, use:

```sh
python3 lab/scripts/environment_binding.py migration-plan --runtime --state-root /absolute/permanent/kuchdesk
python3 lab/scripts/environment_binding.py migrate --state-root /absolute/permanent/kuchdesk --approve 'approve-environment-migration <planSHA256>'
```

`--state-root` explicitly selects the verified repository holding the original
private forwarding receipts when the old worktree is gone. Review both files
before approval. The command rechecks their hashes, VM machine ID, cluster UID
and profile identity under a lock. It preserves complete receipt bytes and the
original marker bytes in binding metadata, writes mode-0600 environment state,
and commits the new marker last. Interrupted identical plans can resume;
changed evidence and conflicting destinations fail closed. Migration starts
no services and changes no network configuration. Then install only the
approved Host components from the permanent checkout.

## Service YAML and credentials

Service configuration may use multiple YAML files when each has a distinct
purpose. Keep tracked `values.yaml` for non-secret chart defaults. Put private
machine-specific overrides and credentials in ignored files with mode `0600`,
using the filenames and install path documented by that service: charts that
consume a private overlay use `values.local.yaml`; services that provision
credentials separately use `auth.local.yaml` and a namespaced Kubernetes
Secret. Do not assume a local file is not passed to Helm. Tracked examples may
show key names and placeholder values only. Exclude private files from chart
packages with `.helmignore` when they are under a chart directory. Split files
by purpose; do not duplicate settings across YAML files without a consumer
that requires it.

## Connection contracts

Observability ingestion is separate from browser panels. Alloy OTLP/HTTP accepts
traces, metrics and logs on published TCP20006. On 2026-10-20 a synthetic
metric sent to `127.0.0.1:20006/v1/metrics` returned HTTP200 and appeared in
Prometheus. The publication Service allows the configured Colima host networks,
containerd bridge and k3s Pod CIDR. This keeps access available to the local
development clients; independent LAN isolation has not been tested. Lima's Mac
listener remains bound to `127.0.0.1`. OTLP/gRPC TCP20007 has a Mac listener and
TCP connectivity, but an OTLP gRPC export is not yet verified; server reflection
is unavailable. Ordinary bridge containers use the verified VM address
`192.168.64.15`; Pods use the internal Alloy Service. For OTLP/HTTP, SDKs append
`/v1/traces`, `/v1/metrics`, or `/v1/logs` to the base endpoint. These routes
use plaintext without OTLP authentication and are for local development. Do not
substitute `grafana.local.dev` or `alloy.local.dev` as a collector endpoint.
See [Alloy's signal contract](../lab/apps/observability/alloy/README.md) and
[bounded k6 dashboard access](../lab/apps/observability/k6/README.md).
Temporary task-owned loopback forwards used for acceptance are not additions
to the stable publication catalog.

### Observability acceptance status — updated 2026-10-20

Catalog entries TCP20006 and TCP20007 are enabled and published by the
Helm-owned `observability/alloy-publication` release. The 2026-10-20 source-range
update added only the active Colima host network and k3s Pod CIDR to the existing
local ranges. Mac HTTP OTLP now completes end-to-end to Prometheus. Both Lima
listeners bind to Mac `127.0.0.1`; the gRPC listener has TCP reachability only.
ServiceLB VM-side exposure is separate from Mac binding; independent LAN
isolation remains untested.

Signal ownership is explicit: Kubernetes Pod stdout is collected by Alloy's
Kubernetes reader; Mac and ordinary-container logs require an application OTLP
logs exporter and are not read from host files or a containerd socket. OTLP
metrics can be pushed through Alloy to Prometheus. Prometheus pulls Mac metrics
from the Mac exporter on its Colima interface and pulls Pod exporters through
their Kubernetes Services/ServiceMonitors. Ordinary bridge containers do not
have generic automatic Prometheus discovery: expose a metrics endpoint only
through an explicitly configured per-service scrape target, or use OTLP metrics.
Do not grant Alloy/Prometheus access to the containerd socket for discovery.

Prometheus scrapes the Mac exporter at `192.168.64.1:9101`; the launchd
listener is bound only to that Colima bridge address, and `up{job="macos-host"}`
returned1. Disposable Pod and bridge-container exporters were also scraped
successfully and removed. Ordinary-container scraping is explicit per target,
not automatic discovery. Prometheus now uses a bound 10Gi `local-path` PVC with
three-day retention. The prior ~849MiB `emptyDir` TSDB was intentionally not
migrated under the approved fresh-storage transition; persistence across a
subsequent Prometheus Pod replacement has not been tested.

HTTP traces, metrics and logs were sent from Mac, an ordinary nerdctl bridge
container and a Kubernetes Pod to the VM publication or internal Service. The
tagged metric series and synthetic trace/log records were queried from
Prometheus, Tempo and Loki. After the approved fresh TSDB rollout, the Mac
exporter scrape is live; previous OTLP acceptance samples were lost with the old
emptyDir and are not claimed as surviving. gRPC was accepted and found in Tempo
from the Mac-to-VM address; gRPC from bridge containers and Pods is not verified.
After that rollout a fresh Pod OTLP metric was queried successfully through
Prometheus and the authenticated Grafana API. On 2026-10-20 the same historical
sample remained queryable after replacing only the Prometheus Pod; its 10Gi PVC
and PV identities stayed Bound. A separate Mac-loopback OTLP metric was also
queried at value3. The earlier empty query came from a synthetic OTLP datapoint
without `timeUnixNano`; it was a malformed test fixture.
Loki indexes bounded service/namespace/environment/client labels, not the
per-run acceptance ID. Pod stdout collection remains separate from host and
container application OTLP logs.

| Client | Address | Meaning |
| --- | --- | --- |
| Mac application / independent host Test Runner | `127.0.0.1:<published-port>` | Automatic Lima forwarding while the VM and service run; no application kubectl tunnel. |
| Ordinary nerdctl bridge container | Verified VM address and published port | Its localhost is the container. Current route is eth0 via 10.4.0.1 to 192.168.64.15. |
| Kubernetes Pod | `service.namespace.svc.cluster.local:<internal-port>` | CoreDNS/ClusterIP remain internal; authenticate and satisfy workload policy. |
| Web panel | Dedicated `https://<service>.local.dev` | Traefik, Technitium DNS and the local CA, separate from raw protocols. |

| Endpoint / Helm owner | Mac endpoint | Container endpoint on this installation | Pod endpoint | Access |
| --- | --- | --- | --- | --- |
| NATS / `platform-storage/nats` | `nats://127.0.0.1:20000` | `nats://192.168.64.15:20000` | `nats.platform-storage.svc.cluster.local:4222` | Scoped account/user; JetStream; local-development plain transport. |
| MailDev SMTP / `vero/maildev` | `127.0.0.1:20001` | `192.168.64.15:20001` | `finance-maildev.vero.svc.cluster.local:1025` | No SMTP AUTH/TLS; development sink, outgoing relay disabled. |
| PostgreSQL / `platform-storage/postgresql` | `127.0.0.1:20003` | `192.168.64.15:20003` | `postgresql.platform-storage.svc.cluster.local:5432` | Authenticated `vero`, database `finance`; no superuser privilege. |
| Redis / `platform-storage/redis` | `127.0.0.1:20004` | `192.168.64.15:20004` | `redis.platform-storage.svc.cluster.local:6379` | ACL user `vero`, DB0, approved Development key prefixes; default user off. |
| RabbitMQ / `platform-storage/rabbitmq` | `amqp://127.0.0.1:20005` | `amqp://192.168.64.15:20005` | `rabbitmq.platform-storage.svc.cluster.local:5672` | Authenticated `finance` user/vhost, no administrator tag. |

All five TCP protocols passed real operations from all three locations on
2026-10-19. MailDev captures were verified by exact synthetic recipient, subject
and body; post-restart greeting/EHLO also passed in all locations. These checks
do not make Finance deployed or tested. See the
[current acceptance evidence](../.kuchdesk/diagnostics/infrastructure-acceptance-2026-10-19.md).

Shared network access preserves service authorization. Other projects need their
own database/schema, Redis ACL/keyspace, RabbitMQ vhost or NATS account/subjects.
Existing credentials are referenced in ignored mode-0600
`lab/apps/platform/<service>/auth.local.yaml` and matching namespaced Secrets;
never paste their values or credential-bearing URLs into commands or reports.
PostgreSQL/Redis server definitions preserve Finance's source behavior; application
ENV and server/container ENV are distinct contracts.

### Shared protected MailDev API — runtime verified (2026-10-19)

SMTP and the authenticated HTTP API serve all current cluster Pods, ordinary
Colima bridge containers and Mac applications. No Finance label or ephemeral
ServiceLB Pod IP is an API access identity. Mac endpoints are SMTP127.0.0.1:20001
and HTTP127.0.0.1:20002; ordinary containers use the verified VM address with
those ports. Pods use SMTP `finance-maildev.vero.svc.cluster.local:1025` and API
`kuchdesk-maildev-api.kube-system.svc.cluster.local:20002`.

The API uses existing panel BasicAuth through Traefik; backend1080 remains
restricted to Traefik Pods. Existing HTTPS inbox UI policy stays separate.
Both MailDev publications use explicit local LoadBalancer source ranges and
no NodePorts. These protect the VM path separately from Lima's Mac loopback
binding. HTTP is unencrypted local-development transport; passwords are never
removed or placed in URLs. No relay is configured. All three modes passed authenticated API checks and one exact synthetic SMTP
capture/deletion each. See the [shared MailDev acceptance report](../.kuchdesk/diagnostics/maildev-shared-access-2026-10-19.md).

### Verified UI domains (2026-10-19)

| URL | Login | Route owner |
| --- | --- | --- |
| https://grafana.local.dev/ | Existing Grafana credentials | observability/grafana, unchanged |
| https://maildev.local.dev/ | Shared panel login | kube-system/kuchdesk-ui |
| https://rabbitmq.local.dev/ | Separate native monitoring-only login | kube-system/kuchdesk-ui |
| https://technitium.local.dev/ | Existing Technitium admin credentials | kube-system/kuchdesk-ui |
| https://prometheus.local.dev/ | Shared panel login | kube-system/kuchdesk-ui |
| https://alertmanager.local.dev/ | Shared panel login | kube-system/kuchdesk-ui |
| https://alloy.local.dev/ | Shared panel login | kube-system/kuchdesk-ui |
| https://traefik.local.dev/dashboard/ | Shared panel login | kube-system/kuchdesk-ui |

All names resolve normally on this Mac to `192.168.64.15`; the existing wildcard
TLSStore/certificate provides trusted HTTPS. New routes, static assets, safe
APIs, native logins and incorrect/missing-login rejection were tested. MailDev's
actual WebSocket upgrade passed101 without inspecting captured messages.
An ordinary Pod was rejected403 on all seven new routes even with a forged
X-Forwarded-For header. Direct MailDev1080 and RabbitMQ15672 remain denied to
non-Traefik Pods. These tests are recorded in the
[activation report](../.kuchdesk/diagnostics/ui-domain-activation-2026-10-19.md).
Interactive browser clicking and independent LAN testing are not claimed.

The new [route-only chart](../lab/core/ingress/ui/README.md) consumes this same
catalog through `helm-service.py ui`, preserves all backend Helm owners, and
contains no workloads, Services, PVCs or credential Secrets. New panel logins
use username `kuchdesk-ui`; the shared panel password and the separate RabbitMQ
password are different. Actual credentials are in ignored mode0600
`lab/core/ingress/ui/auth.local.yaml` and
`lab/apps/platform/rabbitmq/auth.local.yaml` (`uiClient`), respectively.
The UI chart's `.helmignore` excludes its private auth file from Helm archives
and release history. Kubernetes receives only bcrypt htpasswd records in
`kuchdesk-ui-basic-auth` Secrets for the shared login. Authorization headers are
removed before proxying those requests. Grafana/Technitium credentials are
unchanged. RabbitMQ's application identity still has no management tag; the new
monitoring identity has no application-resource permissions.

`externalTrafficPolicy: Local` now preserves Mac source `192.168.64.1` at
Traefik. New routes allow only that `/32`, use the default remote address rather
than trusting forwarded headers, and require the approved logins. Kubernetes
also requires a VM health listener for this LoadBalancer mode: its separately
approved `healthCheckNodePort` is pinned to30689. This serves unauthenticated
Service health on the VM with a wildcard bind; no Mac listener/forward is
configured for it. It is a management exception outside the application pool.
The existing VM443 socket is shared ingress; the route filter does not make
that socket loopback-only or establish independent LAN isolation.

Preserve the existing DNS, CA, trust store, Grafana Ingress, internal Service
names and raw protocol publication. Do not widen the route filter to the former
masquerade address10.42.0.1. Another installation must verify its actual Mac
source before selecting a source range. No native UI is invented for
PostgreSQL, Redis or NATS; Loki/Tempo remain accessed through Grafana.

## Catalog and dynamic publication

[lab/service-access.yaml](../lab/service-access.yaml) is the single typed catalog.
TCP **20000–20099** and UDP **20000–20099** are separate, configurable pools.
Each contains 100 numbers; this is not a fixed architecture/service-count limit.
Only active discovered publications acquire bindings. The Mac does not reserve
these ranges. Non-Kubernetes consumers may reserve an allocation in the same
catalog without claiming a Service or Helm owner; these reservations participate
in range, loopback-bind and collision validation but are not rendered or applied
as Kubernetes resources. Validate duplicates per protocol and preserve stable
allocations.

1. Identify the existing Helm owner, internal Service, target port and protocol.
2. Check Mac listeners, VM sockets, existing Services and ServiceLB hostPorts.
   Allocate a free catalog entry; never kill another owner or silently remap.
3. Publish only the intended protocol through that owner's canonical values and
   Helm-owned Service. ServiceLB's high `service.port` maps to the unchanged
   workload target port. Do not publish every Pod port or move DNS/API/SSH/HTTPS.
4. Run `python3 lab/scripts/service_access.py plan`, use the owner's
   `lab/scripts/helm-service.py` workflow, then verify ready EndpointSlices,
   actual listener ownership and a real protocol response from each client mode.
5. Recheck Grafana DNS/HTTPS and clean only task-owned acceptance fixtures.

Finance's ordinary container uses catalog reservation
`finance-container-development/app` on TCP20008, bound on the Mac to
`127.0.0.1:20008` and forwarded by the existing `kuchdesk-fresh` Lima port
range to container port3000. This reservation is distinct from Finance's
Kubernetes Service/Ingress and does not publish a K8s Service. The Finance
Compose wrapper verifies the selected container receipt and the port mapping;
the Lima host agent owns the Mac loopback listener while the profile runs.

The persistent [Lima policy](../lab/colima-lima-port-forwards.yaml) binds eligible
TCP/UDP application ports to Mac **127.0.0.1**, followed by an all-interface
ignore before generated catchalls. Management paths have explicit exceptions.
Lima filters IP/protocol/port, not Kubernetes ownership: unrelated VM listeners
in the range can qualify. Mac loopback does not constrain VM-side ServiceLB
reachability; direct VM access works from this Mac and the bridge container.
Independent LAN access/isolation was not tested and is not claimed.

### Installed-version UDP limitation

Native gRPC UDP request/reply passed on a distinct UDP fixture port20095; TCP
used20094. The same numeric TCP/UDP allocation is valid in the catalog, but Lima
2.2.1's Kubernetes discovery deduplicates by number without protocol, suppressing
one mapping when both use the same number. This is an upstream runtime limitation,
not a change to the separate-pool contract. Use distinct numbers on this installed
version until that discovery defect is corrected and verified. SSH is not UDP
forwarding or recovery. UDP collision recovery remains unsupported: native gRPC
did not retry after release, and no supported UDP replay command/API was found.
A socket send alone is not a UDP health check.

## Installation, lifecycle and recovery

### Environment ownership binding (source implementation)

The v2 ownership contract is independent of a checkout path. The fixed
`kuchdesk-fresh` profile and `colima-kuchdesk-fresh` context use a private
`<COLIMA_HOME>/_kuchdesk/kuchdesk-fresh/binding.json` (mode 0600 in directories
of mode 0700). The profile's `.kuchdesk-managed` becomes a private JSON reference
containing `version: 2` and its `environmentId`. The binding pins the repository
identity, resolved Colima home, profile device/inode, VM `machineID`, and the
`kube-system` namespace UID as `clusterUID`. Copying a profile/binding to a new
directory does not enroll it. Owner, file type, symlink and permission checks
fail closed.

The configured root remains a locator for helper code and the catalog. Local
Git metadata must identify its exact Git root and the expected origin
`github.com/amirtaherkhani/kuchdesk`; a caller-supplied repository label is
insufficient. A relocated checkout or another checkout with that verified
origin can use the same environment binding. This does not install an independent
helper or make an unavailable configured code/catalog root work: Host Agent
configuration still needs a valid locator, updated separately when necessary.

Read local ownership with:

```sh
python3 lab/scripts/environment_binding.py status --root /absolute/kuchdesk-checkout
```

Successful JSON includes `version`, `repository`, `environmentId`, `profile`,
`context`, `clusterUID` and derived `stateDirectory`. It proves local ownership,
not live runtime health. Clients must compare independent live cluster identity.
`fresh-local.py start` validates local ownership before startup, so a stopped VM
does not need to answer a live probe. After startup, it compares VM/cluster pins
before seeding images. Stop and forwarding-policy operations require local
ownership; forwarding cleanup/recovery and publication also compare live pins.

A legacy plain checkout-path marker always returns `migration-needed`; it is
never silently adopted, even when the old checkout still exists. New-profile
enrollment and migration activation are deliberately unavailable in this source
repair. Existing legacy profiles cannot start/stop or change forwarding through
these revised helpers until a separately reviewed migration is implemented and
authorized. No marker, installed helper or running environment has been migrated
by this source change.

Produce a read-only migration inventory with:

```sh
python3 lab/scripts/environment_binding.py migration-plan --root /absolute/kuchdesk-checkout
```

The plan records exact marker/state hashes, old and new locations, profile
identity, required runtime evidence, blockers, `planSHA256` and an exact proposed
approval string. `executable: false` is intentional: there is no apply command.
A deleted legacy checkout blocks state inventory; absence of access is not
evidence of an empty retry budget. Review/recover the original
`.kuchdesk/forwarding-ssh.json` and `forwarding-recovery.json` before activation.
A future implementation must require approval of the exact reviewed digest,
hold an exclusive migration lock, recheck all evidence, preserve original marker
bytes and state files, and atomically activate private binding/marker writes.
It must copy complete SSH ownership receipts and retry/backoff counters without
resetting them, and must not start/reconcile the VM or perform network operations.
An interrupted activation must fail closed and remain recoverable from the
preserved evidence. Approval of a plan alone performs no operation.

For a new installation, use the canonical private Colima profile with
`runtime: containerd`, Kubernetes enabled and `portForwarder: grpc`. First
inventory the Lima home: the override is shared by its profiles. The installer
refuses an unknown override or another profile; do not relocate/recreate a VM
to evade this scope check. Install with
`python3 lab/scripts/colima_forwarding.py apply`, then start the selected profile
using `colima --profile <profile> start --activate=false --port-forwarder grpc`.
Install reviewed charts with their canonical values and Secret references, not
secret-valued Helm flags. Verify platform/image digests before a pull or build.

A changed forwarding policy/forwarder needs a planned stop/start. Ordinary
service publication inside an already active range does not. Never repeat a
consumed restart. A deliberately stopped VM stays stopped; clients reconnect
when it starts. Login launchd jobs require the user session; this guide does not
claim availability while Colima is off. Preserve PVCs and database data.

The existing Host Bridge's private `forwardingRecovery: true` enables bounded
reconciliation, separate from read-only status calls. It validates profile/VM
identity, catalog allocation, exact Helm owner, selector/target, readiness and
listener ownership. It leaves conflicts untouched. After release it can restore
an absent **TCP** mapping through the existing native Lima SSH control socket,
even with gRPC active. For a v2 binding, a mode-0600
`<stateDirectory>/forwarding-ssh.json` receipt binds
that mapping to its publication UID, SSH master PID/start time and exact port.
Publication removal/reallocation cancels only that recorded mapping. Never
cancel a foreign mapping. `<stateDirectory>/forwarding-recovery.json` preserves bounded
retry/backoff state; three attempts stop an unresolved incident. New UID/removal
or two healthy observations clear the incident. These are runtime ownership
receipts, not another configuration store or forwarding daemon.
Both Python helpers and the Host Bridge reconciler resolve this same validated
directory. Legacy checkout state remains untouched pending explicit migration.

One approved live TCP collision/recovery/removal cycle passed after revised Bridge
activation. Native gRPC TCP and UDP collision retry failed independently. Do not
restart Colima as routine recovery, and do not imply TCP repair supports UDP.
The current policy prevented the historical privileged TCP53 escape: startup
explicitly excluded guest wildcard/loopback IPv4/IPv6 port53. Connected outbound
UDP DNS sockets are not local port53 listeners.

## Agents and status

`python3 lab/scripts/service_access.py status`, authenticated Host Bridge
`GET /v1/service-access`, and installed MCP `kuchdesk_service_access_status` read
the same catalog/Service/EndpointSlice/listener evidence. Reports include
namespace/service, protocol, internal/published ports, selected profile/context,
Mac/container endpoints, owner, timestamp/freshness, backend evidence, probe level
and recovery state. TCP-only reachability is not protocol authentication. UDP
binding-only observations are not request/reply acceptance. Unknown, stale,
missing, conflict and disabled states must remain explicit. MCP inputs select
catalog entries; they are not arbitrary network-probe URLs. A Pod cannot inspect
Mac files/listeners; the existing Host Bridge supplies host observations.

The Host DNS Agent remains RunAtLoad + StartInterval scheduled one-shot, no
KeepAlive. The managed job uses `-target-ip auto`: each run resolves the
selected Colima ingress address and derives the Lima loopback API port from
the matching kubeconfig endpoint, with normal certificate verification.
If Colima is stopped or the endpoint mismatches, that run fails closed and
the next scheduled run retries. No old VM address or port is pinned.
It changes neither shared kubeconfig nor credentials. Real launchd reconciliation
and later scheduled runs passed; interactive kubectl alone would not prove this.
The separate Host Bridge intentionally retains KeepAlive.

The Host Bridge server certificate is issued by the existing `local-dev-ca`
ClusterIssuer for `host.lima.internal` and the Colima host gateway IP. The Mac
already trusts that CA; the Kubernetes Agent receives the CA certificate, not
the server private key. A mode-0600 host LaunchAgent checks the cert-manager
Secret at startup and every 30 minutes, refreshes the Host Bridge leaf/key
files, and restarts only the owned Bridge when the leaf changes. Its Kubernetes
calls use the existing Lima loopback API listener: the API port comes from the
selected kubeconfig, its address must match the selected VM, and normal TLS,
credentials, and the node/VM machine-ID check remain enforced. This avoids the
launchd `no route to host` failure on the direct VM API address. A natural
launchd timer run and the restored normal launcher passed on 2026-10-20;
certificate rotation was not forced. It uses only the
`kuchdesk-fresh` profile and `colima-kuchdesk-fresh` context. A new Mac still
needs one-time OS authorization to trust a new development CA; macOS does not
permit silently adding system trust.

Installed MCP stdio initialize/discovery/status passed. This existing chat still
has no KuchDesk tools. Fully quit and reopen Codex, start a new chat loading the
existing `kuchdesk` registration, then discover/call
`kuchdesk_service_access_status`. Do not create a duplicate registration.

## Testing boundaries

Keep host-run tests separate from shared application dependencies when they drop
schemas, mutate Redis ACLs, restart brokers or otherwise destroy state. Use
isolated databases/keyspaces/vhosts/accounts and disposable brokers as required.
Unit tests remain cluster-independent. No Kubernetes test configuration is needed
for the external host Test Runner. Finance endpoint/credential alignment,
derived configuration, migrations/seeds and API acceptance remain its later
cutover; preserve its source definitions until that replacement is verified.

Primary contracts: [Lima forwarding](https://lima-vm.io/docs/config/port/),
[K3s ServiceLB](https://docs.k3s.io/networking/networking-services#service-load-balancer),
[Lima2.2.1 discovery](https://github.com/lima-vm/lima/blob/v2.2.1/pkg/guestagent/guestagent_linux.go).

Fresh permanent-identity bridge/Pod probes and the isolated-test resource plan are recorded in the [Finance prerequisites report](../.kuchdesk/diagnostics/finance-prerequisites-2026-10-19.md). They do not establish Finance application acceptance.
