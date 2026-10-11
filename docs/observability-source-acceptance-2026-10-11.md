# Observability source acceptance — 2026-10-11

The continuation preserved existing work and corrected normal deployment sources to retain approved observability settings. Source baseline was `c2849255bc28e043d4c86c466a64fa30c2b0ece8`. Source fixes were committed as `0b38b6a0b2a34f3edff078caf4bb0ebc5f90a9f4`, integrated into main as `9d3a8635daa821a635a7f14420bbbbf86ea761e5`, and remotely verified. After explicit startup approval, Docker Desktop started through the supported macOS application flow. Kubernetes `/readyz` returned `ok`, its node was Ready at v1.36.1, Infisical v0.151.0 was Ready with health HTTP 200, all 11 observability Pods became Ready, and read-only Doctor passed 7/7. The separately approved Host interval check changed only 60 → 90 → 60 and restored the exact original policy bytes and mode 0600. No rollout, identity/grant/credential change, image deletion or backup occurred.

## Source changes and verification

The homelab Grafana profile renders anonymous Editor access in `Main Org.`. This preserves approved shared access on the existing ingress, including dashboard/folder deletion and alert/notification editing; server and user administration remain excluded. Grafana annotation expiry is excluded.

Canonical profiles specify Loki 72h, Prometheus 3d and Alertmanager 72h. Tempo remains 72h. Normal monitoring commands use the process-scoped Helm 4 wrapper and reviewed renderer, preserving all alert definitions while aligning API-server availability records and dashboard labels to three days. The wrapper rejects a caller-supplied renderer. Missing or drifted targets stop rendering without emitting a candidate.

Loki pins the approved sidecar 2.8.1 index and sets local ruler storage to `/rules`. The pinned 7.0.0 chart render proves this directory equals the main container mount, sidecar mount and watcher `FOLDER`. Fresh live `/config` returned 200 and confirmed effective ruler storage `/var/loki/rules` while mounts and watcher use `/rules`; retention is 3d and `/ready` returns 200. The rule API returned 400, so no fresh loaded-group count is claimed. This source correction is verified offline; it is not deployed and has no live file-delivery/evaluation acceptance yet. The temporary localhost forward was closed after inspection.

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

## Acceptance and remaining live gates

| Work | Current evidence | Required next step |
|---|---|---|
| Host policy hot reload | Passed source tests and bounded live 60 → 90 → 60 scheduling acceptance. Exact original policy and mode 0600 restored; PID unchanged. | Evidence is serialized journal-write timing, not exported cycle timestamps. Broader process/identity/Pod environment reload remains outside this bounded test. |
| Loki rule delivery | Canonical chart render maps `/rules` consistently; live mounts and watcher also use `/rules`, but live ConfigMap has no explicit local directory. No labelled rule ConfigMap exists. | Refresh effective configuration and remaining rule-source metadata, then obtain exact one-Pod correction/fixture approval. Verify delivered file, loaded group, healthy evaluation, recent ingestion and exact fixture cleanup. |
| Traefik namespace cutover | Pinned offline render reproduced all six source objects and exact reviewed target; six guards passed. Live revision 1/source binding/manual Service UID unchanged; target namespace absent. Proxy IP now 172.18.0.2. | Complete allocator/IP and direct-consumer evidence with current allocation. Obtain exact cutover approval before controller, Service or binding changes. |
| Image consolidation | All four selected arm64 recovery sources verified after one bounded Blackbox retry. Fresh 98-object/75-CRI-container scan preserves the old sidecar ControllerRevision; other three targets had no observed references in that scope. | Host container listing remains incomplete; Helm rollback manifests were not refreshed. Complete those reference checks before any separately approved retirement. No runtime repull or deletion acceptance is claimed. |

The Host bridge retains PID 1389 and its single-writer journal lock; health returned 200 using its existing public certificate. Policy authority remains `apply=true`, `createK8Identity=true`, `repairRevocations=false`; bindings and all other fields were preserved. The first 180-second observer saw no writes, then reconciliation resumed. Source backoff can reach 15 minutes; the initial stale ledger alone did not identify its cause. The interval test monitored atomic journal replacements, grouped writes separated by a two-second idle period, checked settled entries, and compared consecutive burst ends. These are reconciliation-write checkpoints, not direct `Cycle.ObservedAt` timestamps or Doctor request times. No Host restart or diagnostic binary installation occurred.

Live interval acceptance completed at 12:08:37 UTC:

| Policy stage | Consecutive reconciliation burst-end spacing |
|---|---|
| Baseline 60 seconds | 61.704 seconds |
| Changed to 90 seconds | 92.406 seconds |
| Restored to 60 seconds | 61.815 seconds |

All four sampled bursts had five observed atomic writes and three settled journal entries. Exact original bytes and permissions were restored, every non-interval field was preserved, and the process stayed PID 1389. This verifies the implemented live scheduling boundary without claiming universal hot reload or direct export of the controller's report timestamp.

Traefik preflight uses chart 39.0.7, SHA-256 `55a7384ae3a69af13e7dd7f898304a1f9890791e6712774635cd3a2df34da4c8`, preserving chart/image versions, Helm release namespace, ClusterRole rules and IngressClass. The candidate changes the Traefik ServiceAccount, ClusterIP Service and Deployment namespaces, plus the existing ClusterRoleBinding's ServiceAccount subject namespace. Fresh manual Service UID remains `d03a6a04-3e84-4d6a-a33b-d5061760d302`, resourceVersion was `865757`, and NodePorts remain 32366/32013. Its proxy IP changed from the earlier 172.18.0.6 to 172.18.0.2 after startup. Same-IP recovery cannot be assumed. Live consumer inventory and a reviewed allocator/IP decision remain necessary. A five-minute abort gate is a bound, not a completion estimate.

Automatic approval review rejected an unnecessary full Secret-object query. The safer ConfigMap-only inspection succeeded. No Secret inventory or value extraction was used; remaining Secret metadata coverage must use an approved metadata-only interface.

## Bounded image recovery result

The 11:44:53 UTC read-only registry check verified three sources and timed out for Blackbox. One bounded retry at 12:07 UTC also verified Blackbox. Each successful check retrieved and SHA-256 verified the root index and selected linux/arm64 manifest, config and all layers. Bytes were streamed and discarded; no runtime-store pull, backup or removal occurred. Other-platform layers were not fetched. This is source-byte verification, not proof of a successful CRI repull.

| Image | Exact root digest | Result |
|---|---|---|
| Old sidecar | `a6b3f707f883108376514489a94d6629109a327b2978e1d826cd104c4ca436df` | Verified selected arm64 source bytes; protected by rollback history |
| Alternate k6 runner | `e66db15b860113878fa74670e31f5e274830b7b6e42c8bff28b2f2d86a257603` | Verified selected arm64 source bytes |
| Alternate k6 starter | `b97b8e32867a553b7f866acd1e0a2ac91af81480c04c12a6ee55df3ae99d2ad6` | Verified selected arm64 source bytes |
| Old Blackbox | `a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d` | Initial timeout; exact selected arm64 source bytes verified on bounded retry, no fallback |

Fresh consumer evidence covers 98 built-in workload/history objects and 75 running/exited CRI containers. It found the old sidecar's `loki-94f54fd8c` history and no selected references for the other three targets. Host `ps -a` remains empty despite Docker info counting five containers, and Helm rollback manifests were not refreshed. This is not a complete deletion preflight. Preserve the tested k6 cohort, active sidecar, controller histories and existing images until a separately authorized action passes all checks.

Infisical upgrade, its preflight/backup, and SDK upgrade are dismissed from this task. They are not deferred execution items. The old Nova session and task worktree remain preserved, including unrelated untracked acceptance-helper sources and cache files.
