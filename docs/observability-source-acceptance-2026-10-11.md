# Observability source acceptance — 2026-10-11

The continuation preserved existing work and corrected normal deployment sources to retain approved observability settings. Source baseline was `c2849255bc28e043d4c86c466a64fa30c2b0ece8`. After explicit startup approval, Docker Desktop was started through the supported macOS application flow. Kubernetes `/readyz` returned `ok`, its node was Ready at v1.36.1, Infisical v0.151.0 was Ready with health HTTP 200, all 11 observability Pods became Ready, and read-only Doctor passed 7/7. No rollout, policy edit, credential/grant change, image deletion or backup occurred in this source continuation.

## Source changes and verification

The homelab Grafana profile renders anonymous Editor access in `Main Org.`. This preserves approved shared access on the existing ingress, including dashboard/folder deletion and alert/notification editing; server and user administration remain excluded. Grafana annotation expiry is excluded.

Canonical profiles specify Loki 72h, Prometheus 3d and Alertmanager 72h. Tempo remains 72h. Normal monitoring commands use the process-scoped Helm 4 wrapper and reviewed renderer, preserving all alert definitions while aligning API-server availability records and dashboard labels to three days. The wrapper rejects a caller-supplied renderer. Missing or drifted targets stop rendering without emitting a candidate.

Loki pins the approved sidecar 2.8.1 index and sets local ruler storage to `/rules`. The pinned 7.0.0 chart render proves this directory equals the main container mount, sidecar mount and watcher `FOLDER`. Previously observed effective ruler storage was `/var/loki/rules` while shared mounts used `/rules`. This source correction is verified offline; it is not deployed and has no live file-delivery/evaluation acceptance yet.

Verification passed:

- Full `lab` Go suite with both checksum-pinned chart archive variables supplied; chart-dependent tests ran without skips.
- Full `host-agent` Go suite with cached dependencies. Local HTTP test servers required permitted sandbox escalation; the rerun passed all packages.
- Renderer contract: unchanged objects and alerts, restricted rule/dashboard edits, and rejection of missing, duplicate, drifted or already-transformed inputs.
- Bash/Ruby syntax and Git whitespace checks. Ruby emitted the existing unrelated iStats extension warning; it did not prevent execution.

No production Host code changed. [Pinned inputs and repeat commands](../lab/migrations/observability-retention/README.md#normal-workflow-validation) document source tests. Verification does not upgrade dependencies or install a runtime binary.

## Earlier completed actions

These are preserved execution evidence from earlier in this task. Current post-startup readiness above does not repeat all earlier mutation acceptance checks:

| Action | Recorded result |
|---|---|
| Three old node-agent configs | Supported non-force removals; current config and historical state preserved |
| Two old Blackbox ReplicaSets | Exact UID/resourceVersion removals; current ReplicaSet and Pods preserved |
| Loki sidecar | Pinned 2.5.0 → 2.8.1, release revision 1 → 2; ready Pod |
| Retention | Loki revision 2 → 3, monitoring 1 → 2; 72h/3d/72h accepted |
| Ten platform-storage claims | Exact approved claims deleted through normal provisioner reclamation; eight other claims preserved; 60Gi was provisioned capacity, not measured disk savings |
| Previous source integration | Baseline `c284925` pushed and remote-verified before these fixes |
| k6 smoke run | One VU, one HTTP 200 request, all checks passing, temporary resources cleaned |

Retention acceptance completed at 10:01:03 UTC. Prometheus 3.13.1 reloaded with the same Pod and earlier head data: loaded configuration was 3d and TSDB retention was 259200 seconds, despite its flags endpoint retaining 5d. Alertmanager replaced its emptyDir Pod with 72h and zero silences before and after. All 218 rules were healthy, all 133 alert definitions stayed unchanged, and 29 dashboard UIDs were preserved. Only the API-server dashboard body changed. Loki recent ingestion remained observable; physical expiry of old chunks was not proven. Tempo, PVCs and existing ingress/service exposure stayed protected.

Separate approved Grafana Editor acceptance completed at release revision 4. Temporary dashboard create/edit/read/delete checks passed and cleanup was verified; all 29 real dashboards remained. Administration checks returned 403. This access approval does not authorize another live access change.

## Open live work and exact gates

| Work | Current evidence | Required next step |
|---|---|---|
| Host policy hot reload | Existing source tests pass; policy still 60 seconds, mode 0600, unchanged authority flags. A 180-second post-startup ledger observation saw zero writes. | Approved 60 → 90 → 60 test still needs an observed healthy controller baseline and actual cycle timing evidence. No interval edit was attempted against the stale baseline. Doctor request timestamps alone do not prove controller cadence. |
| Loki rule delivery | Canonical chart render maps `/rules` consistently; live mounts and watcher also use `/rules`, but live ConfigMap has no explicit local directory. No labelled rule ConfigMap exists. | Refresh effective configuration and remaining rule-source metadata, then obtain exact one-Pod correction/fixture approval. Verify delivered file, loaded group, healthy evaluation, recent ingestion and exact fixture cleanup. |
| Traefik namespace cutover | Pinned offline render reproduced all six source objects and exact reviewed target; six guards passed. Live revision 1/source binding/manual Service UID unchanged; target namespace absent. Proxy IP now 172.18.0.2. | Complete allocator/IP and direct-consumer evidence with current allocation. Obtain exact cutover approval before controller, Service or binding changes. |
| Image consolidation | Three selected arm64 recovery sources verified; Blackbox timed out. Old sidecar still appears in Loki ControllerRevision `loki-94f54fd8c`. | Complete current live/exited/rollback inventory. Docker info reports five containers while supported `ps -a` returns none, so current container visibility is incomplete. Blackbox recovery remains incomplete. This check authorizes no deletion. |

The Host bridge retains PID 1389 and its single-writer journal lock. Policy remains `apply=true`, `createK8Identity=true`, `repairRevocations=false`; none was changed. The ledger predates recovery; the bounded observation found no new reconciliation writes. Source backoff can reach 15 minutes, but stale ledger state alone does not identify the current cause or prove controller failure. Per-project ledger writes cannot be presented as whole-cycle timestamps without validated grouping and controller evidence. No Host restart or diagnostic binary installation is included.

Traefik preflight uses chart 39.0.7, SHA-256 `55a7384ae3a69af13e7dd7f898304a1f9890791e6712774635cd3a2df34da4c8`, preserving chart/image versions, Helm release namespace, ClusterRole rules and IngressClass. The candidate changes the Traefik ServiceAccount, ClusterIP Service and Deployment namespaces, plus the existing ClusterRoleBinding's ServiceAccount subject namespace. Fresh manual Service UID remains `d03a6a04-3e84-4d6a-a33b-d5061760d302`, resourceVersion was `865757`, and NodePorts remain 32366/32013. Its proxy IP changed from the earlier 172.18.0.6 to 172.18.0.2 after startup. Same-IP recovery cannot be assumed. Live consumer inventory and a reviewed allocator/IP decision remain necessary. A five-minute abort gate is a bound, not a completion estimate.

Automatic approval review rejected an unnecessary full Secret-object query. The safer ConfigMap-only inspection succeeded. No Secret inventory or value extraction was used; remaining Secret metadata coverage must use an approved metadata-only interface.

## Bounded image recovery result

The 11:44:53 UTC read-only registry check retrieved and SHA-256 verified each successful root index and selected linux/arm64 manifest, config and all layers. Bytes were streamed and discarded; no runtime-store pull, backup or removal occurred. Other-platform layers were not fetched. This is source-byte verification, not proof of a successful CRI repull.

| Image | Exact root digest | Result |
|---|---|---|
| Old sidecar | `a6b3f707f883108376514489a94d6629109a327b2978e1d826cd104c4ca436df` | Verified selected arm64 source bytes; protected by rollback history |
| Alternate k6 runner | `e66db15b860113878fa74670e31f5e274830b7b6e42c8bff28b2f2d86a257603` | Verified selected arm64 source bytes |
| Alternate k6 starter | `b97b8e32867a553b7f866acd1e0a2ac91af81480c04c12a6ee55df3ae99d2ad6` | Verified selected arm64 source bytes |
| Old Blackbox | `a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d` | Timeout; verification incomplete, no fallback |

Consumer evidence predates later retention Pods and is not a fresh deletion preflight. Preserve the tested k6 cohort, active sidecar, controller histories and existing images until a separately authorized action passes fresh checks.

Infisical upgrade, its preflight/backup, and SDK upgrade are dismissed from this task. They are not deferred execution items. The old Nova session and task worktree remain preserved, including unrelated untracked acceptance-helper sources and cache files.
