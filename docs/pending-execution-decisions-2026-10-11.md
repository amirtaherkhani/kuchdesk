# Remaining execution decisions — 2026-10-11

This is the active list after the 08:00–08:14 UTC audits. The completed agent/k6 namespace stages, bounded config-reload verification and prior image removals remain in their historical reports. This continuation made no live rollout, deletion, grant or credential change. Infisical upgrade/SDK work is excluded; Colima/K3s research is handled elsewhere.

## 1. Three old agent node images

All three still exist in node CRI, with zero desired/zero-replica or retained running/exited container consumers in the fresh scan. Historical Helm revisions still reference old payloads. The eight-host-image discard approval does not cover this node set.

| Exact node config | Current action |
|---|---|
| `sha256:8db48db0a69c465446c66409324452505d057177e9f62820da23d1375ef7de20` | Preserve pending explicit discard |
| `sha256:ed99259af2eed7786d10e5bf5858c3e533bef4cfc55519c9ee71ee5bf5e9d82b` | Preserve pending explicit discard |
| `sha256:ea564bdeb8a8caabfa30aaf3a9aa0603bc8cb57c5fe1676742858ffdc546ef94` | Preserve pending explicit discard |

[Exact aliases and historical rollback sources](old-agent-image-discard-approval-2026-10-11.md#optional-node-scope--excluded-from-host-only-approval) remain the approval scope. Original host indices are gone; verified registry platform payloads do not restore all original aliases/index/attestation content.

Decision: permanently discard exactly these three configs and their listed aliases, accepting loss of those local historical sources and reduced rollback options, with no backup. Keep current node config `47db29c7d13f3e5f53afa5647d92209d1cc55b475a99b01977164d623d1dc696`, all containers and registry content. Before action, recheck full IDs, aliases and consumers; use supported non-force removal and stop on a dependency refusal.

## 2. Live image consumers and rollback references

| Scope | Reviewable proposal | Remaining gate |
|---|---|---|
| Loki rules sidecar | [Pinned 2.8.1 overlay and checks](../lab/migrations/image-consolidation/README.md#loki-rules-sidecar); exact installed-chart render preserves nine objects and changes only its container image | Approve the Loki-only Pod replacement and possible ingestion/query interruption; refresh pinned recovery proof, then verify watcher/rule delivery and Loki health before retiring 2.5.0 |
| Blackbox v0.27 rollback history | Two zero-replica sets below retain the old image; current v0.28 workload is 1/1 Ready | Approve permanent retirement of only these two rollback records; subsequently recheck all image/container references and recovery before a separate exact old-image removal |
| k6 helper cohort | Existing smoke TestRun already pins initializer/runner 2.0.0 and starter-v1.5.0; installed CRD supports the fields | Approve one existing one-VU/one-iteration Grafana-health smoke TestRun, its temporary Jobs/Pods and metrics write, then verify completion/cleanup before proposing alternate-cache removal |

Exact Blackbox rollback records in `observability`, both owned by Deployment `dns-blackbox` UID `eeb330be-562d-4744-b3cc-096a463d1568`:

| ReplicaSet | UID | Observed resourceVersion |
|---|---|---|
| `dns-blackbox-5bf7d9c49f` | `51432170-4564-4ac9-8974-cdb43bc002ae` | `574594` |
| `dns-blackbox-68d49f5b95` | `4a1e17b8-3904-4ed5-b217-b82d45c33a1b` | `575060` |

Both use `prom/blackbox-exporter:v0.27.0@sha256:a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d`. Re-read UIDs/resourceVersions and zero replicas immediately before any approved delete; use API preconditions. Preserve the current v0.28 ReplicaSet and all Pods. This decision does not authorize deletion of exited containers, Helm revisions, sidecar ControllerRevisions or images.

## 3. Three-day history retention

[Exact proposal and data-loss wording](observability-retention-plan-2026-10-11.md#exact-action-time-approval) are prepared. Helm 4's process-scoped renderer now passes offline comparison against all 95 monitoring objects; all 133 alerts remain unchanged. Tempo already has 72h retention and needs no rollout.

Remaining access/runtime gates: Grafana's rules API returns 401, so existing authenticated read access is needed to inventory rule lookbacks and history backend. Loki's ConfigMap specifies an empty runtime override file, but current config/runtime/rule endpoints return 500/404/400 and mounted-file inspection lacked an available utility. Resolve effective configuration/rule access before apply; do not extract credentials or install a helper merely to bypass this limit.

Decision after those gates pass: approve Loki 120h→72h and monitoring Prometheus 5d→3d / Alertmanager 120h→72h with the reviewed availability rules/dashboard. Eligible older Loki chunks may be permanently deleted; required Pod replacements can lose **all current Prometheus metrics and all Alertmanager silence/deduplication state**, including recent data. Expect brief interruptions, repeat-notification risk and history warm-up. Configuration rollback cannot restore history. No backup or raw purge is included. Grafana annotation expiry needs its own explicit category decision and renderer-token preservation proof.

## 4. Ten storage claims

[Exact PVC/PV UIDs, backing paths and consumer limits](platform-storage-deletion-preflight-2026-10-11.md) are refreshed. The same ten remain Bound with reclaim `Delete`; no direct claim consumer exists in the 84-object scan. The node-exporter root mount still exposes their paths read-only, and offline/manual/future consumers remain an evidence limit. Provisioned capacity is 60Gi, not measured recoverable space.

Decision: permanently delete only `platform-storage` claims `data-kafka-0`, `data-minio-0`, `data-mongodb-0`, `data-nats-0`, `data-nats-1`, `data-nats-2`, `data-object-storage-0`, `data-postgresql-0`, `data-rabbitmq-0` and `data-redis-0`, with their exact linked UIDs. Accept the normal local-path provisioner's deletion of their PVs and backing directories. Choose no backup explicitly, or define a separate backup scope first. Recheck UID/resourceVersion and consumers immediately before the exact destructive confirmation; delete with API preconditions only after approval. Preserve all eight other claims, including both registry claims, Technitium, observability and Infisical storage. No command has run.

## 5. Remaining namespaces

[Updated dependency map](namespace-migration-plan.md#dependency-refresh--2026-10-11-0814-utc) covers cert-manager/Traefik, Technitium and the registry. Their moves require new reviewed cutover scopes: cert-manager's CA namespace/webhook/RBAC references, DNS volume/continuity and Host references, and the registry's two-claim ownership plus node-pull/localhost forwarding path.

Only the completed agent/k6 migrations were approved. Prepare one component's exact resource/ownership and recovery plan before requesting its live cutover; source namespaces, CA deliveries and data stay preserved. A new target namespace alone would not resolve these dependencies.
