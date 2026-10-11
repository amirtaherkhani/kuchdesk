# Namespace migration preparation

Prepared from live metadata on 2026-10-10 and reverified on 2026-10-11. The approved k6 and agent stages are complete; the remaining mappings are preparation. The current profile remains operational. See the [namespace architecture](namespace-proposal.md).

## Current state

- No running NATS, RabbitMQ or Kafka Pods, Deployments, StatefulSets or Services were found; the current Helm inventory contains no matching releases. Do not infer running brokers from retained PVCs or monitoring links.
- Infisical's `platform-secrets/redis-master` is 1/1 ready and stays internal. Its PostgreSQL and all other `platform-secrets` resources stay in place.
- Doctor passes 8/8, enrollment reports two projects. The mapped agent, k6, Traefik, cert-manager, registry and Technitium workloads were ready at inspection.
- Preserve all system namespaces, all ten bound `platform-storage` PVCs, and all other bound claims. No namespace rename, broker installation or data move is included.

## Mapping recorded before the k6 stage

Names below stay the same unless a separate reviewed plan says otherwise. This records the original mapping; k6 and the agent have since moved to their approved targets. Other target resources have not been created by this work.

| Current objects / ownership | Intended namespace | Dependencies and gate |
|---|---|---|
| Agent Deployment, Service and ServiceAccount `kuchdesk-agent` now in `platform-tools`; Helm record remains `development-tools` | Completed approved stage | Revision 7; original image/config/RBAC rules preserved. Two existing Secrets/public CA copied, source copies retained. See the scoped renderer workflow below. |
| `observability-tests`: Deployment `k6-operator-controller-manager`, Service `k6-operator-controller-manager-metrics-service`, ServiceAccount `k6-operator-controller`; Helm release `k6-operator` | `platform-tests` | Role/RoleBinding `k6-operator-leader-election-role` / `k6-operator-leader-election-rolebinding`; ClusterRoleBindings `k6-operator-manager-rolebinding`, `k6-operator-metrics-auth-rolebinding`; metrics discovery and test manifests. Requires approval of exact new subjects, with no broader rules. |
| `platform-system`: Deployment/Service/ServiceAccount `traefik`; manually managed Service `traefik-lan`; Helm release `traefik` | `platform-networking` | ClusterRoleBinding `traefik-platform-system`, ingress class/provider scope, LAN ports, TLSStore and certificate Secret references. Preserve existing traffic until replacement routing passes checks. |
| `platform-system`: Deployments/Services/ServiceAccounts `cert-manager`, `cert-manager-cainjector`, `cert-manager-webhook`; Helm release `cert-manager` | `platform-networking` | ClusterRoleBinding subjects, webhook Service namespace and CA trust, leader election, cluster-resource namespace and private CA. Move only in an explicitly approved PKI/security plan. |
| `technitium`: StatefulSet `technitium`, Services `technitium` and `technitium-lan`, claim `data-technitium-0`; no Helm ownership annotation observed | `platform-networking` eventually | Bound data, DNS continuity, admin Secret and Host DNS job namespace/service references. No volume move/rebind/delete authorized. Keep current instance until backup/restore and cutover are separately approved. |
| `platform-system`: Deployment `kuchdesk-local-registry`, mounted claim `kuchdesk-local-registry`; no Helm ownership annotation observed | `platform-tools` eventually | Bound registry data and local port-forward/LaunchAgent target. Preserve additional bound claim `kubedeck-local-registry`; its consumer/ownership requires review. No claim operation authorized. |
| Certificates `development-tools/local-dev-tls`, `observability-tests/local-dev-tls` | Respective target namespaces only if still needed | Namespaced certificate Secret delivery and consumers must be reviewed. Existing certificates remain; provisioning/copying credentials is security-gated. |
| `platform-system/Certificate local-dev-ca`; ClusterIssuers `local-dev-ca` and `local-dev-selfsigned` | Keep current placement during initial preparation | CA Secret `local-dev-ca` and cert-manager cluster-resource namespace are coupled. Never relocate/export the private key as routine namespace preparation. |
| `development-tools/ServiceAccount kubedesk-infisical-reader`, its token-request roles/bindings | Keep in `development-tools` initially | Independent auth anchor; moving the agent does not require moving this account. Optional later move to `platform-tools` requires the coordinated security change below. |
| `observability` resources, including Grafana and `InfisicalSecret/grafana-admin` | Unchanged | Grafana remains with observability. Dedicated future broker/database UIs follow their functional service; generic management UIs belong in `platform-tools`. |
| `platform-secrets`, system namespaces, `platform-storage` and ten claims | Unchanged | Explicit preservation boundary. Shared database and messaging services are future designs, not migrations of these claims. |

cert-manager has cluster-scoped bindings for cainjector; controller approve, certificates, certificate-signing requests, challenges, clusterissuers, ingress-shim, issuers and orders; and webhook subject-access reviews. Its MutatingWebhookConfiguration and ValidatingWebhookConfiguration both target `platform-system/cert-manager-webhook`. A release move must review their exact rendered ownership and subjects; do not install competing controllers or adopt cluster-scoped resources blindly.

## Infisical namespace-bound authentication

Fresh readback confirms Kubernetes Auth allows exactly namespace `development-tools` and account `kubedesk-infisical-reader`, with 900-second token TTL/max TTL and no persisted reviewer JWT.

- `observability/InfisicalSecret grafana-admin` points to that account and the unchanged backend Service in `platform-secrets`.
- ClusterRoleBinding `kubedesk-infisical-token-reviewer` targets that account.
- Role/RoleBinding `kubedesk-infisical-token-request` grants the Operator account `platform-secrets/infisical-opera-controller-manager` TokenRequest only for that named reader.
- The Helm-owned `development-tools/infisical-opera-manager-role` and `infisical-opera-manager-rolebinding` also target the Operator and include the narrowed TokenRequest rule. Review both rule sources together; retain the resource-name restriction.

Leaving this anchor in place avoids an immediate auth migration. Any later move must explicitly approve the new account, exact RBAC subjects/rules, Infisical allowed namespace/name, Operator namespace scope and Grafana account reference as one bounded change. Do not temporarily allow all namespaces or unrelated accounts. Preserve the same Infisical Host Admin/K8 read-only model.

## Configurable project mapping

Kubedesk Platform owns shared infrastructure. Each managed project has its own Infisical project and configurable namespace/environment mapping keyed by the stable project ID. See the proposal's concrete project IDs and configurable `vero` namespace example for the `vero-finance` project. Neither the project name nor `vero` is a mandatory namespace name. There is no mandatory `app-` prefix, no substitution of `finance` for `vero-finance`, and no automatic namespace provisioning from Infisical discovery. Provisioning defaults disabled until explicit policy is approved.

Preparation points identified in the repository:

- `lab/site.json`: CA namespace and certificate namespace list; preserve current defaults until cutover.
- `lab/apps/platform/infisical-operator/values.yaml`: Operator namespace scope.
- `lab/apps/observability/k6/tests/smoke-test.yaml` and `lab/README.md`: k6 target namespace and commands.
- `lab/core/exposure/docker-desktop/traefik-lan.yaml` and ingress values: LAN exposure and routing.
- Host installation: `KUCHDESK_HOST_AGENT_NAMESPACE` maps to `-namespace`; update through configuration, not a source-code project-name substitution. Check registry port-forward targets separately.
- Agent Helm release namespace, Service DNS, monitoring targets, certificate references and Secret sources must be rendered and compared together.

Do not switch the checked-in active profile piecemeal before these dependencies are ready. Future alternate profiles should render offline and be reviewed before live use. Supported metadata/policy reload remains available; process settings, identity bindings and Kubernetes Pod settings may require a controlled restart or rollout.

## Execution gates and rollback

1. **Preparation:** render proposed release manifests offline with existing versions, compare object ownership and RBAC, and verify namespace/project mapping. No active profile or live resource changes are made by this document.
2. **Security approval required before live changes:** exact RBAC subject/rule updates, especially the agent's cluster-admin binding; Secret/credential delivery; Kubernetes Auth account/namespace changes; webhook/PKI/CA changes; any network-policy or access expansion. Current authorization allows preparation, not silently granting these privileges.
3. **Data/irreversible approval required:** migration or deletion/rebinding of any bound claim, deletion of source namespaces or persistent workloads, and destructive release cleanup. The ten retained platform-storage claims remain excluded. Backups must be restorable, not merely present.
4. **One service at a time after approval:** preserve existing versions and permissions, use an approved handover rather than concurrent conflicting controllers, and keep the old endpoint until target validation. Namespace changes require replacement/redeployment, not renaming a namespace in place. Helm namespace moves require explicit release/cluster-resource ownership planning; a same-name install in another namespace is not a complete migration.
5. **Health gates:** compare ready replicas and endpoints; Doctor 8/8 with two enrolled projects; authenticated agent API/metrics; k6 controller and metrics target; DNS wildcard and recursive resolution; ingress HTTPS and certificate readiness; Grafana auth sync; positive reader login/read plus denied write and denied unrelated-account login. Verify registry image pull without introducing test credentials or changing data.
6. **Rollback:** record original release revisions, non-secret configuration, routing/Service references and RBAC subjects before cutover. Keep originals until checks pass. On failure, restore the approved original release/subjects/references and traffic endpoint; re-run the same health checks. A Helm rollback alone does not restore moved data or external Infisical auth configuration. Do not delete target or source data as an automatic rollback step.

The approved k6 stage completed in 11.8 seconds, Helm revision 2 retained in observability-tests. Workloads and metrics are ready in platform-tests; Doctor passed 8/8. CRDs, ClusterRole rules and all ten retained PVCs were preserved. No rollback was needed. See the [stage workflow](../lab/migrations/k6-platform-tests/README.md). The agent stage also completed, as recorded below; the remaining migrations await their individual gates.

## Repository Helm inventory

Read-only inventory on 2026-10-10 found three tracked `Chart.yaml` files and no tracked `Chart.lock` or Helmfile release definition. None of these three charts declares a `dependencies` list. Upstream dependencies are not thereby proven absent: their remote chart source is outside this local inventory. Release instructions live in the guides; generated overlays are separate from authored charts.

| Repository file / path | Classification and purpose | Current installed release |
|---|---|---|
| `kuchdesk-agent/chart/Chart.yaml`, `values.yaml` | Locally authored cluster-agent chart 0.13.0 | `development-tools/kuchdesk-agent`, deployed |
| `lab/apps/observability/grafana/Chart.yaml`, `values.yaml`, `values.homelab.yaml` | Vendored upstream Grafana chart 12.7.3 with local configuration/templates | `observability/grafana`, deployed |
| `lab/tests/infisical-helm-probe/Chart.yaml`, `values.yaml` | Locally authored opt-in test chart 0.1.0 | No current installed release found |
| `lab/apps/observability/alloy/values.yaml` | Upstream `grafana/alloy` configuration | `observability/alloy`, chart 1.10.1, deployed |
| `lab/apps/observability/k6/values.yaml` | Upstream `grafana/k6-operator` configuration | `observability-tests/k6-operator`, chart 4.5.0, deployed |
| `lab/apps/observability/kube-prometheus-stack/values.yaml` | Upstream `prometheus-community/kube-prometheus-stack` configuration | `observability/monitoring`, chart 87.15.1, deployed |
| `lab/apps/observability/loki/values.yaml` | Upstream `grafana/loki` configuration | `observability/loki`, chart 7.0.0, deployed |
| `lab/apps/observability/tempo/values.yaml` | Upstream `grafana/tempo` configuration | `observability/tempo`, chart 1.24.4, deployed |
| `lab/apps/platform/cert-manager/values.yaml` | Upstream `cert-manager/cert-manager` configuration | `platform-system/cert-manager`, chart v1.21.0, deployed |
| `lab/core/ingress/traefik-docker-desktop-values.yaml` | Upstream `traefik/traefik` configuration | `platform-system/traefik`, chart 39.0.7, deployed |
| `lab/apps/platform/infisical/values.yaml` | Upstream `infisical-helm-charts/infisical` configuration; enables internal Redis, disables MongoDB | `platform-secrets/infisical`, chart 0.4.2, deployed |
| `lab/apps/platform/infisical-operator/values.yaml` and `README.md` | Upstream Secrets Operator configuration plus documented rendering of a supplied chart archive with narrowed TokenRequest RBAC | `platform-secrets/infisical-operator`, chart v0.11.11, deployed |
| `lab/apps/platform/infisical/manifests/postgresql.yaml` | Plain Kubernetes Service/StatefulSet manifest for Infisical PostgreSQL; not a Helm chart or shared database definition | StatefulSet exists in `platform-secrets`; no independent PostgreSQL Helm release found |
| NATS, RabbitMQ, Kafka, shared PostgreSQL/Redis/MongoDB | No corresponding local chart, dependency declaration or values/release definition found | No current shared-service/broker release found |
| metrics-server | No local chart/values file found in this bounded inventory | `kube-system/metrics-server`, chart 3.14.0, deployed |

The upstream release commands and pinned versions are recorded in `lab/README.md`; the Operator's specialized rendering/upgrade instructions are in its own README. No new chart was fetched or installed for this inventory. Retained broker/database PVCs are not chart definitions or proof of current releases.

Operational backup defaults are [native local exports without an added encrypted container](backup-defaults.md), outside Git with restrictive permissions and restoration checks. No backup was created for the k6 move. Infisical upgrade work is paused; see the [storage audit and checkpoint](platform-storage-audit-2026-10-10.md).

## Approved scope: stateless KuchDesk agent

The original preflight found release `kuchdesk-agent` revision **6**, chart **0.13.0**, source Deployment 1/1 ready and target namespace absent. The exact scope below was approved and subsequently applied; the execution checkpoint records current verification.

Approved stage scope:

- Create `platform-tools`; move Deployment, Service and ServiceAccount `kuchdesk-agent` from `development-tools`, retaining the same image, configuration and names. Keep the Helm release record in `development-tools` using a reviewed namespace post-renderer, as with k6.
- Copy existing Secrets `kuchdesk-agent-auth` and `kuchdesk-host-bridge-token` to the target namespace through an in-memory private workflow; reuse their existing data without printing, rotating or placing it in Git. Retain the source copies for rollback. Copy ConfigMap `kuchdesk-host-bridge-ca`, which contains the public trust certificate. No private key, certificate issuance, CA/Issuer or Infisical identity change is included.
- Change only the namespace of the `kuchdesk-agent` ServiceAccount subject in ClusterRoleBinding `kuchdesk-agent-admin` and RoleBinding `kube-system/kuchdesk-agent-coredns`. Preserve `cluster-admin` and the existing CoreDNS Role/rules exactly; do not add privileges. This is an explicit security approval gate, including the one binding in kube-system; no CoreDNS ConfigMap or other system resource changes are proposed.
- Keep `development-tools`, the Infisical reader ServiceAccount, its RBAC/auth restrictions, Grafana's reference, all PVCs and other workloads unchanged. Update only relevant client namespace/port-forward references. The internal Service DNS moves to `kuchdesk-agent.platform-tools.svc`; do not silently assume external clients already use it.

Expect a brief interruption to the agent API/SSE stream, followed by reconnect. Its in-memory operation history is not durable; confirm no active operations before stopping it and defer if the idle state cannot be established. No unrelated application service should be restarted.

Health gates: target 1/1 Ready, `/healthz` and `/readyz`, authenticated cluster snapshot and SSE reconnect, Host bridge/Infisical metadata reads, Doctor 8/8, unchanged CoreDNS configuration, and unchanged Infisical/K8 permissions. Allow five minutes for readiness. On failure, roll Helm back to revision **6** in `development-tools`, restore the original two RBAC subjects and client references, and verify source readiness/health. Preserve the old namespace and Secret copies; no automatic data cleanup.

Offline manifest comparison prepared from the exact current release changes only the three resource namespace fields and two RBAC subjects; the CoreDNS Role remains identical. Secret values were not read for preparation. The target Secret/ConfigMap copies and two subject changes were executed under the specific approval above; the user also expressly accepted interruption of potentially active operations.

## Historical idle gate and its resolution

The exact agent move was subsequently approved, including the two Secret copies and two RBAC subject changes. Execution preflight rechecked context `docker-desktop`, release revision **6** with status `deployed`, source Deployment **1/1**, and absence of `platform-tools`. No live mutation followed.

The required idle-operation gate could not be established. The last 15 minutes of agent logs contained no entries, but this is insufficient: exec and port-forward audit logging happens only when handlers return; asynchronous jobs log when execution starts, leaving queued work silent. The API exposes status only for a known job ID, with no global active/queued-operation inventory or admission/drain control. Source review independently confirmed the gap. Time limits and quiet logs do not prove that current operations are absent.

The healthy source agent remains running. No namespace, credential copy, RBAC change, endpoint change or Helm revision was applied; the five-minute rollback gate was not entered. The user then explicitly accepted interruption of potentially active exec/tunnel/queued operations. This resolved the idle gate without adding a drain endpoint or expanding migration scope. Infisical upgrade remains paused and retained PVCs remain excluded.

## Agent stage completed and reverified

The approved move completed on 2026-10-10; target readiness took **6.4 seconds**, within the five-minute gate, and rollback was not needed. Fresh 2026-10-11 inspection confirms Helm revision **7** deployed with metadata in `development-tools`, target Deployment **1/1**, and no source Deployment/Service/ServiceAccount. Stored revision 6 transformed by the maintained renderer exactly equals revision 7: only three resource namespaces and two binding subject namespaces differ. Image, environment and RBAC rules are identical. Source and target copies of both Secrets and the public CA match in memory; no credential values were printed or placed in Git. The Infisical reader identity remains in `development-tools`.

After starting the existing Docker Desktop engine, the node became Ready. Initial enrollment checks returned `project_denied` and Doctor 7/8 with a warning; the existing controller recovered as runtime startup settled. Fresh final checks pass API health/readiness, bearer authentication and denied unauthenticated access, snapshot, SSE connect/reconnect, metrics API, in-cluster Service DNS/ClusterIP health, two-project Host bridge metadata reads and Doctor **8/8**, enrollment `ready`. No login, grant or identity change was needed. The ten retained storage claims remain Bound with the identities documented in the [fresh deletion preflight](platform-storage-deletion-preflight-2026-10-11.md).

Follow the [agent renderer workflow](../lab/migrations/agent-platform-tools/README.md) for future upgrades: workload namespace `platform-tools`, Helm metadata `development-tools`. The generic Host deploy workflow currently lacks the required post-renderer and separate target rollout namespace, so ordinary use on this migrated release would revert the move. Helm chart NOTES likewise report the release namespace and are not the current Service DNS.

## Remaining namespace dependencies

The k6 and agent stages are complete. Remaining moves need their own resource/ownership plan and applicable approval; no further live stage is started:

- Traefik/cert-manager to `platform-networking`: map Helm records, cluster-resource ownership, admission webhooks, Service routing and certificate issuers/consumers before any cutover. This is a networking/PKI gate, not a routine namespace edit.
- Technitium to `platform-networking`: map its bound data, service/admin endpoint, Host LaunchAgent namespace and private Secret references. DNS continuity and storage recovery require a separate approved handover.
- Registry to `platform-tools`: map the active bound registry claim and the second retained registry claim's owner, image recovery dependencies and the localhost/node pull path. Neither registry claim belongs to the ten-claim deletion set.

Retain source namespaces and old certificate deliveries until their actual consumers are reviewed. Observability and Infisical stay in their existing functional namespaces. Shared databases/brokers remain designs, with no new identity, grant, credential, network, PKI or storage rollout implied. Infisical upgrade and SDK work remain deferred.

## Verification-helper preservation

The active worktree's two untracked activation-helper source files were copied byte-for-byte to a private task archive under the Codex task-artifacts directory. Copies use `.go.txt`, with SHA-256 manifest, directory mode0700 and files0600. Independent source review found no embedded actual credential literal. No environment/session files, credentials, binaries or database data were included.

This is historical acceptance-test source, not a supported production command. It has hard-coded installation IDs/paths, mutable authentication/probe modes, best-effort cleanup and direct session reads with fewer safeguards than production storage code. Preserve it for evidence; do not execute it without fresh review. Original files and worktree remain intact; no cleanup was performed.
