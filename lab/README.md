# Fresh Colima/K3s local profile

This directory prepares a new KuchDesk installation. It does not migrate data or credentials from Docker Desktop or kind. Live execution is authorized by the final execution prompt; the final report records actual runtime evidence.

## Inputs

| Service | Direct YAML consumed at installation |
| --- | --- |
| Colima/K3s | ignored colima-k3s.local.yaml; tracked colima-k3s.example.yaml documents the schema |
| K3s Traefik | core/ingress/traefik-k3s.yaml, a HelmChartConfig consumed by the K3s Helm controller |
| Protected UI routes | core/ingress/ui/values.yaml plus service-access.yaml; existing credential Secret references only |
| K3s CoreDNS | K3s bundled CoreDNS; the Agent uses the new cluster's kube-system/coredns ConfigMap for explicitly enabled aliases |
| Technitium | apps/platform/technitium/values.yaml and ignored values.local.yaml |
| MailDev | apps/platform/maildev/values.yaml |
| PostgreSQL / Redis / RabbitMQ | apps/platform/SERVICE/values.yaml and ignored auth.local.yaml Secret manifests |
| NATS JetStream | apps/platform/nats/values.yaml; ignored apps/platform/nats/auth.local.yaml supplies the namespaced Secret |
| cert-manager | apps/platform/cert-manager/values.yaml |
| Grafana | apps/observability/grafana/values.homelab.yaml and ignored values.local.yaml |
| Prometheus Stack | apps/observability/kube-prometheus-stack/values.yaml |
| Loki | apps/observability/loki/values.yaml |
| Tempo | apps/observability/tempo/values.yaml |
| Alloy | apps/observability/alloy/values.yaml |
| k6 Operator | apps/observability/k6/values.yaml |
| Host DNS monitoring | apps/observability/host-dns-monitoring/dns-blackbox.yaml, dns-probes.yaml, host-scrape.yaml |
| KuchDesk Agent | ../kuchdesk-agent/chart/values.yaml and ignored values.local.yaml |

Helm consumes the listed canonical values directly. Vendored charts load their own base values; private overlays are mode 0600. `scripts/helm-service.py SERVICE --context colima-kuchdesk-fresh` prints a command plan without running it. For upstream charts, supply `--chart` with the reviewed local archive/directory; the script does not download or invent a chart version. `--apply --confirm SERVICE/CONTEXT` applies only the selected owner/context after reviewing its plan. It first lints values and checks bundled CoreDNS/Traefik, then uses Helm 4 rollback-on-failure. For Traefik it applies only the K3s HelmChartConfig; for CoreDNS it inspects the bundled deployment. The legacy Traefik wrapper rejects upgrades.

`service-access.yaml` is the canonical typed publication catalog. NATS20000,
MailDev SMTP20001, PostgreSQL20003, Redis20004 and RabbitMQ20005 have verified
Mac/container/Pod TCP paths; inbox API20002 remains disabled. Native gRPC forwards
discovered TCP/UDP ports, and the existing Bridge repairs owned TCP collisions.
Installed-version UDP limitations and the exact current connection matrix are
in the [canonical access guide](../docs/local-service-access.md).

`colima-lima-port-forwards.yaml` is the canonical native Lima rule. It became effective after the one approved 2026-10-19 restart: only discovered guest TCP/UDP listeners in their independent 20000–20099 pools are forwarded to Mac loopback, with verified management exceptions; other automatic application forwarding is ignored before generated catchalls. Guest TCP 53 was explicitly not forwarded at startup. The range does not reserve Mac ports. Preserve SSH, Kubernetes API and socket paths, VM DNS and direct VM HTTPS. `fresh-local.py start` checks the owned profile and persistent override; a later override change requires a separate restart. A stopped profile remains stopped. Check collisions without killing or silently remapping another listener. The override would affect any future profile in the same Lima home, so review it before creating another profile.

Host/MCP reads and edits canonical files through `configyaml/`; Helm consumes those same files and supplies Pod ENV and Secrets. The Kubernetes Agent has no direct access to Mac files and mounts no source directories. File edits preserve unrelated keys; applying a changed value still requires reconciliation. Private overlays remain ignored and mode 0600.

site.json keeps the intended local.dev domain, grafana.local.dev host, new private CA settings, and kubedesk-local-tls Secret name. Run go run ./cmd/render-site to produce new Certificate, ClusterIssuer, TLSStore, and DNS probe configuration for this profile. Grafana's host and TLS name are repeated in its directly consumed values.homelab.yaml and must match. The renderer does not install charts or issue certificates.

## Fresh initialization contract

The current profile uses Colima containerd runtime plus K3s v1.35.0+k3s1 (an observed selection, not a universal version requirement), with `k3sArgs: []` so bundled Traefik remains enabled. The private profile selects 4 CPUs, 8 GiB RAM, a new 80 GiB data disk, reachable VM addressing and a scoped read-only VM build-source mount (never mounted into Pods). It contains no service credential values. Do not add old persistent-volume paths or point the profile at a previous VM. The profile name prepared for later use is `kuchdesk-fresh`; its Kubernetes context is `colima-kuchdesk-fresh`.

The profile's `after-boot` provision sets `fs.inotify.max_user_instances` to 1024 before container runtimes start, and writes the same setting to `/etc/sysctl.d/99-kuchdesk-inotify.conf` for guest boot persistence. Keep this provision in the private profile when preparing a new installation. Check the live value with `colima --profile kuchdesk-fresh ssh -- cat /proc/sys/fs/inotify/max_user_instances`; a source setting alone does not prove it loaded after a restart or recreation.

Before later startup, verify Colima's actual configuration root (`COLIMA_HOME`/XDG/default rules), prove this named profile is absent and install the prepared local YAML as that new profile's `colima.yaml`. An existing profile must stop the fresh-initialization procedure, not be reset or reused. These instructions describe a new installation; the current named installation is already active and must not be recreated.

```sh
# Lifecycle commands; never reset/delete an existing installation.
colima --profile kuchdesk-fresh start --activate=false --port-forwarder grpc
colima --profile kuchdesk-fresh status --json
colima --profile kuchdesk-fresh stop
```

`--activate=false` preserves unrelated active contexts. Every kubectl/Helm call selects the named context; image operations use `colima --profile kuchdesk-fresh nerdctl -- --namespace k8s.io`. Do not use Docker Desktop binaries/sockets, node containers or a previous host registry. Only kubectl and Helm clients are required; Colima supplies guest nerdctl and BuildKit. The guest K3s version and runtime must be verified after startup; an installed Colima CLI version is not evidence of a running K3s version.

K3s supplies CoreDNS, Traefik, ServiceLB, metrics-server and local-path storage. Read their live resources before configuration, so a disabled/missing packaged component is a blocker rather than a reason to install a duplicate controller. Create only new claims in the dedicated cluster. Grafana and Technitium explicitly select local-path; the other new claims use the new cluster's default class. Prometheus/Alertmanager ephemeral defaults are not evidence of persistent retention.

Install cert-manager, then newly generated CA/certificate resources from `cmd/render-site`, Traefik configuration, Technitium, observability and the Agent in dependency order. Operator CRDs/ServiceMonitors require their existing chart dependencies; the plan is not a full dependency orchestrator. The local MailDev chart provides a disposable SMTP sink in `vero`. NATS uses the pinned official chart archive and canonical values in `apps/platform/nats`; its private auth manifest is applied as a namespaced Secret before Helm. Shared PostgreSQL and Redis charts copy the Finance server contracts into `apps/platform/postgresql` and `apps/platform/redis`; `apps/platform/rabbitmq` supplies its development broker. Their fresh credentials and installation state are recorded in the current diagnostics. Finance source definitions remain until a separately verified cutover.

## DNS, ingress and TLS boundaries

Inside Kubernetes, CoreDNS resolves `SERVICE.NAMESPACE.svc.cluster.local`. The Agent discovers these service names through the Kubernetes API. macOS and ordinary containerd containers do not inherit Kubernetes Service DNS or ClusterIP routing. Use the published-port paths in the canonical access guide for applications; temporary diagnostic port-forwards are separate. `host.lima.internal` denotes the Mac from Lima/Colima; its resolution/reachability from Pods and the optional macOS metrics scrape must be checked later. It is not a Kubernetes service address or a replacement for the verified VM ingress IP.

The Technitium chart exposes DNS on TCP/UDP53 with its local-path claim. Its internal admin Service is retained; the approved Mac HTTPS route uses its native login and a source filter. The Host Agent's loopback port-forward path is unchanged. The `*.local.dev` answer must point to the verified reachable Colima ingress address, not the Mac's default-route interface. Shared-mode VM addressing does not establish LAN-wide routing. See the canonical access guide for verified panels and the route-only UI chart.

K3s Traefik owns HTTP 80 and HTTPS 443. `core/ingress/traefik-k3s.yaml` configures HTTP-to-HTTPS redirection without a second Traefik release. Cert-manager creates a fresh private CA and leaf certificates. `site.json` retains `local.dev`, `grafana.local.dev` and `kubedesk-local-tls`; Grafana's direct values must agree. TLSStore is in kube-system and Grafana's leaf Secret is in observability. Certificate issuance, macOS trust, ServiceLB exposure and HTTPS health remain runtime checks. No TCP/TLS passthrough route is defined in the current sources; none is claimed as existing.

## Images and source configuration

The Agent's OCI Dockerfile builds from the repository root. `.dockerignore` excludes private YAML and runtime metadata. Builds run in Colima containerd namespace `k8s.io`, the store visible to k3s. No Docker daemon, CLI, Buildx, registry push or KIND node import is required. Upstream publisher versions/digests stay pinned.

The Host workflow records full source commit, platform and build-input hash. Explicitly authorized uncommitted source uses `git-<12-HEAD>-dirty-<12-input-hash>`; no commit or release is created. Native containerd target metadata provides the manifest/index digest; it is distinct from the config ID. Content completeness is checked before Helm consumes the real identity persisted in canonical Agent YAML. Existing immutable tags are never overwritten; unverified build reuse is rejected.

`python3 lab/scripts/fresh-local.py start` creates only the dedicated profile after an ownership/absence gate and preserves active contexts. `charts` fetches the exact official packages in `charts.lock.json` and checks SHA-256 before reuse. `build-agent` runs the source-identity, preflight, build, digest and Agent rollout workflow. `reconcile` installs existing charts in CRD/dependency order, configures bundled Traefik, and reconciles certificates. `verify` checks the actual node/VM/runtime identity. `stop` is ownership-gated. No command performs global cleanup or data migration.

The replacement MCP is installed and registered only during phase 7 after runtime verification; check the final report rather than assuming this source guide proves activation.

## Offline validation and later gates

Run Go tests in configyaml, host-agent, kuchdesk-agent and lab; run the Python planner/recovery unit tests. Helm lint/template with private inputs must keep rendered output private. Agent chart tests use synthetic image/tag/digest values, which do not establish a deployable current image. Optional upstream archive tests report skips when their reviewed archives are unavailable.

Later acceptance must verify the exact VM/profile/context and containerd k8s.io store, K3s node runtime, scoped VM build-source mount and Host file locks, packaged controllers, fresh claims, image manifest/platform identity, Agent authentication/readiness, internal DNS versus macOS DNS, Technitium wildcard answers, certificate issuance/trust and HTTP/HTTPS responses. No infrastructure or connectivity conclusion follows from these offline checks.

Official references: [Colima v0.10.3 runtime/image contract](https://github.com/abiosoft/colima/blob/v0.10.3/README.md#kubernetes), [Colima configuration](https://github.com/abiosoft/colima/blob/v0.10.3/embedded/defaults/colima.yaml), [K3s packaged networking](https://docs.k3s.io/networking/networking-services), [K3s registry configuration](https://docs.k3s.io/installation/private-registry). Private/HTTP registries need explicit fresh node configuration and are not inferred from an old loopback listener.
