# Observability source acceptance — 2026-10-11

The continuation preserved existing work and corrected normal deployment sources to retain approved observability settings. Source baseline was `c2849255bc28e043d4c86c466a64fa30c2b0ece8`. Source fixes were committed as `0b38b6a0b2a34f3edff078caf4bb0ebc5f90a9f4`, integrated into main as `9d3a8635daa821a635a7f14420bbbbf86ea761e5`, and remotely verified. After explicit startup approval, Docker Desktop started through the supported macOS application flow. Kubernetes `/readyz` returned `ok`, its node was Ready at v1.36.1, Infisical v0.151.0 was Ready with health HTTP 200, all 11 observability Pods became Ready, and read-only Doctor passed 7/7. The separately approved Host interval check changed only 60 → 90 → 60 and restored the exact original policy bytes and mode 0600. That first source/interval stage made no rollout, identity/grant/credential change, image deletion or backup. Separately approved Loki and Traefik live changes completed later on the same date; their current acceptance is recorded below.

## Source changes and verification

The homelab Grafana profile renders anonymous Editor access in `Main Org.`. This preserves approved shared access on the existing ingress, including dashboard/folder deletion and alert/notification editing; server and user administration remain excluded. Grafana annotation expiry is excluded.

Canonical profiles specify Loki 72h, Prometheus 3d and Alertmanager 72h. Tempo remains 72h. Normal monitoring commands use the process-scoped Helm 4 wrapper and reviewed renderer, preserving all alert definitions while aligning API-server availability records and dashboard labels to three days. The wrapper rejects a caller-supplied renderer. Missing or drifted targets stop rendering without emitting a candidate.

Loki pins the approved sidecar 2.8.1 index and sets local ruler storage to `/rules`. The pinned 7.0.0 chart render proves this directory equals the main container mount, sidecar mount and watcher `FOLDER`. The earlier live mismatch (`/var/loki/rules`) was corrected at revision 4 under separate approval. Effective `/config`, actual file delivery, two healthy evaluations, recent ingestion and fixture cleanup now pass. The earlier 400 was from the legacy rule endpoint; `/prometheus/api/v1/rules` supplied the actual evaluation evidence. The temporary localhost forward was closed.

Verification passed:

- Full `lab` Go suite with both checksum-pinned chart archive variables supplied; chart-dependent tests ran without skips.
- Full `host-agent` Go suite with cached dependencies. Local HTTP test servers required permitted sandbox escalation; the rerun passed all packages.
- Renderer contract: unchanged objects and alerts, restricted rule/dashboard edits, and rejection of missing, duplicate, drifted or already-transformed inputs.
- Bash/Ruby syntax and Git whitespace checks. Ruby emitted the existing unrelated iStats extension warning; it did not prevent execution.

The first source/interval stage changed no production Host code. The later authorized deployment-helper changes are recorded below. [Pinned inputs and repeat commands](../lab/migrations/observability-retention/README.md#normal-workflow-validation) document source tests. Verification does not upgrade dependencies or install a runtime binary.

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

## Current acceptance and remaining image gate

| Work | Current evidence | Required next step |
|---|---|---|
| Host policy hot reload | Passed source tests and bounded live 60 → 90 → 60 scheduling acceptance. Exact original policy and mode 0600 restored; PID unchanged. | Evidence is serialized journal-write timing, not exported cycle timestamps. Broader process/identity/Pod environment reload remains outside this bounded test. |
| Loki rule delivery | Revision 4 deployed; `/rules`, delivered file, two healthy evaluations and recent ingestion verified. Exact fixture ConfigMap, file and loaded group all removed. | Complete. Preserve existing retention, images and storage. |
| Traefik namespace cutover | Revision 2 deployed, target ready, source three Helm objects and manual Service absent. HTTP redirects, HTTPS 200, verified TLS, host 80/443, NodePorts and unchanged certificates proved. | Complete. Use the maintained Traefik wrapper for future upgrades. |
| Image consolidation | Post-rollout scan: 99 workload/history objects, 72 running/exited CRI containers, all five host containers, 66 node images. Old sidecar is rollback-referenced; the three other candidates have no observed references within these inventories. | Authorized safe retirement remains blocked on a permitted, trusted image-only historical Helm reference export. No images removed; logical removed bytes 0, actual disk savings unmeasured. Verified source bytes do not establish a runtime repull. |

The Host bridge retains PID 1389 and its single-writer journal lock; health returned 200 using its existing public certificate. Policy authority remains `apply=true`, `createK8Identity=true`, `repairRevocations=false`; bindings and all other fields were preserved. The first 180-second observer saw no writes, then reconciliation resumed. Source backoff can reach 15 minutes; the initial stale ledger alone did not identify its cause. The interval test monitored atomic journal replacements, grouped writes separated by a two-second idle period, checked settled entries, and compared consecutive burst ends. These are reconciliation-write checkpoints, not direct `Cycle.ObservedAt` timestamps or Doctor request times. No Host restart or diagnostic binary installation occurred.

Live interval acceptance completed at 12:08:37 UTC:

| Policy stage | Consecutive reconciliation burst-end spacing |
|---|---|
| Baseline 60 seconds | 61.704 seconds |
| Changed to 90 seconds | 92.406 seconds |
| Restored to 60 seconds | 61.815 seconds |

All four sampled bursts had five observed atomic writes and three settled journal entries. Exact original bytes and permissions were restored, every non-interval field was preserved, and the process stayed PID 1389. This verifies the implemented live scheduling boundary without claiming universal hot reload or direct export of the controller's report timestamp.

Traefik uses chart 39.0.7, SHA-256 `55a7384ae3a69af13e7dd7f898304a1f9890791e6712774635cd3a2df34da4c8`, preserving chart/image versions, Helm metadata namespace, ClusterRole rules and IngressClass. The applied candidate changes only the ServiceAccount, ClusterIP Service and Deployment namespaces, plus the existing ClusterRoleBinding subject namespace. The manual Service was separately replaced using the approved selectors, policies and NodePorts. Its new UID is `a1a01847-2e7e-4834-8a1a-5343720e01c4`; the old UID `d03a6a04-3e84-4d6a-a33b-d5061760d302` is absent. The allocator retained proxy IP 172.18.0.2; future same-IP recovery is not guaranteed. The [maintained workflow](../lab/migrations/traefik-platform-networking/README.md) retains this namespace placement and pins NodePorts in the manual source manifest.

Automatic approval review rejected an unnecessary full Secret-object query. The safer ConfigMap-only inspection succeeded. No Secret inventory or value extraction was used; remaining Secret metadata coverage must use an approved metadata-only interface.

## Approved Loki and Traefik live acceptance

The human approved the exact two scopes at 14:34:37 UTC. Execution was sequential and no rollback was needed. Server dry runs used the pinned charts, `--reuse-values`, `--hide-secret` and reviewed renderers; both candidates matched their scoped resource sets and had no hooks or Secret objects.

| Gate | Verified result |
|---|---|
| Loki deployment | Revision 3 → 4, 14:38:24.837494–14:39:06.910590 UTC (42.073 seconds). Only local ruler directory and rollout checksum changed. One Pod replaced; Loki 3.6.7 and the pinned 2.8.1 sidecar image unchanged. |
| Loki exact fixture | ConfigMap `observability/kuchdesk-loki-file-delivery-acceptance-20261011`, UID `8a59f47f-eee7-4417-b88d-6dcd25da6418`, delivered `/rules/fake/kuchdesk-file-delivery-acceptance.yaml`. File SHA-256 `4124c3da98f2f4b92c72fb63de065850072898138e577405db6aa26a61f7fa8c`. |
| Loki evaluations | Group `kuchdesk_file_delivery_acceptance_20261011` evaluated at 14:41:40.542939253 and 14:41:55.543456135 UTC. `vector(0) > 1`, interval 15s; health `ok`, no last error, alert inactive and no alerts emitted. |
| Loki cleanup and ingestion | Exact UID/resourceVersion deletion; ConfigMap absent, file absent, group absent, final groups/alerts zero. Ready 200, effective directory `/rules`, retention 3d, 512 log entries in the recent one-minute query. Cleanup complete at 14:42:28.714705 UTC. |
| Traefik deployment | Revision 1 → 2; metadata stays `platform-system`, workload namespace `platform-networking`. Target ready and endpoint ready; source Deployment, ClusterIP Service, ServiceAccount and manual Service absent. Same Traefik v3.6.12 image. |
| Traefik cutover gate | Source drain began 14:48:55.211403 UTC; both external routes and forwarding verified by 14:49:11.317654 UTC. **16.106 seconds** drain-to-verification, within five minutes; this is a gate duration, not a measured exact outage. |
| Public routing/TLS | Grafana and Infisical resolve through 192.168.1.100; HTTPS health/status 200 with certificate verification 0. HTTP 301 to the exact HTTPS endpoints. Both leaf fingerprints unchanged: `aafb94e08abcfe302221c7e5ec0eeabaee0fac0d80dc8fa48c400e65c79b649c`. |
| Forwarding and permissions | Proxy IP 172.18.0.2 retained; host bindings `0.0.0.0:80/443`, NodePorts 32366/32013 preserved. ClusterRoleBinding and ClusterRole UIDs retained; roleRef and role rules identical, only approved subject namespace changed. Every other host container retained its ID, state, image and ports. |
| Final read-only checks | At 14:57:51 UTC all scoped Pods and node Ready; all eight captured PVC UIDs/specs and all captured Ingress/TLSStore UIDs/specs unchanged. Doctor at 14:57:56 UTC: healthy, 7/7, no warnings. Infisical stays v0.151.0. |

Private execution evidence lives under `~/.codex/task-artifacts/kuchdesk-approved-loki-traefik-20261011/`: `loki-rollout.json`, `loki-fixture-acceptance.json`, `traefik-cutover.json`, `final-protected-resources.json`, `doctor-final.json` and `post-rollout-image-checkpoint.json`. No fixture or temporary forward remains. No image, retention, credential, PKI, registry, Technitium or persistent-data mutation was included.

The source workflow adds the byte-identical applied Traefik renderer, a process-scoped Helm wrapper, target namespace bootstrap and the manual Service target with its existing NodePorts explicitly pinned. Pinned chart tests first failed with the missing wrapper, then passed after implementation. Direct renderer comparison preserves every field; Helm wrapper comparison allows only Helm's omission of null top-level metadata annotations and the four approved namespace fields. Eight scope-rejection cases, wrapper ownership/command guards and the full lab suite with all three archives passed without skips. Bash/Python syntax and Git whitespace checks pass. No new dependencies or runtime binary installation were needed.

## Bounded image recovery result

The 11:44:53 UTC read-only registry check verified three sources and timed out for Blackbox. One bounded retry at 12:07 UTC also verified Blackbox. Each successful check retrieved and SHA-256 verified the root index and selected linux/arm64 manifest, config and all layers. Bytes were streamed and discarded; no runtime-store pull, backup or removal occurred. Other-platform layers were not fetched. This is source-byte verification, not proof of a successful CRI repull.

| Image | Exact root digest | Result |
|---|---|---|
| Old sidecar | `a6b3f707f883108376514489a94d6629109a327b2978e1d826cd104c4ca436df` | Verified selected arm64 source bytes; protected by rollback history |
| Alternate k6 runner | `e66db15b860113878fa74670e31f5e274830b7b6e42c8bff28b2f2d86a257603` | Verified selected arm64 source bytes |
| Alternate k6 starter | `b97b8e32867a553b7f866acd1e0a2ac91af81480c04c12a6ee55df3ae99d2ad6` | Verified selected arm64 source bytes |
| Old Blackbox | `a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d` | Initial timeout; exact selected arm64 source bytes verified on bounded retry, no fallback |

The 14:52:51 UTC post-rollout consumer scan covers 99 built-in workload/history objects, 72 running/exited CRI containers, all five host containers and 66 node images. Docker's ordinary `ps -a` hides managed containers here; the supported `kind` network peer inventory and individual inspections match Docker info's total of five, all running. The old sidecar is still referenced by `observability/ControllerRevision loki-94f54fd8c` (UID `5f296a4f-1008-4f49-8a4d-9c41e54c2bfa`) and must remain in both stores. The other three candidates have no observed references in these inventories, but retained Helm rollback payload image coverage is incomplete. No deletion occurred, including after the new safe-image-retirement authorization. Removed IDs/tags: none; measured logical removal: 0 bytes; actual disk savings: not measured. Preserve the tested k6 cohort, active sidecar and rollback histories.

Automatic approval review rejected `helm get manifest` because full historical release output can include Secret objects before filtering. No rejected route, alternate full-value/hook export or direct Helm Secret decoding was attempted. No callable image-only historical exporter was exposed. To close the gate, provide a trusted **image-only** export or attestation for the retained release/revision pairs, covering container/initContainer images, hooks and embedded workloads, with exact immutable identities/tags, explicit absence of references to the three candidates, timestamp and matching revision metadata. It must omit raw Secret data and raw release manifests. Fresh list/history metadata at 15:08:41 UTC names 12 retained releases and their exact revision pairs in private `historical-image-evidence-required.json`; it does not inspect payload images. Existing filtered historical receipts plus current revision metadata do not by themselves prove fresh complete image coverage. The project rule also requires runtime-pull or reproducible-rebuild recovery evidence before deletion; source-byte verification is recorded separately above.

## Fresh Alloy image inventory

At 15:05:23 UTC, both stores contain **one distinct Alloy image**, version **v1.17.1**. Two host listing rows are aliases of the same image object, not two payload copies. No older Alloy image is present; the historically removed v1.20.1 image is not inferred from stale reports. No Alloy image or alias was removed in this check.

| Identity | Exact value |
|---|---|
| Host index/object ID and shared repo digest | `sha256:4f6ddc56ffdcf8a6316748fc5162972e20cb301523cac1bb4a31957df733ae9b` |
| Host aliases | `grafana/alloy:v1.17.1`; `grafana/alloy:v1.17.1@sha256:4f6ddc56ffdcf8a6316748fc5162972e20cb301523cac1bb4a31957df733ae9b` |
| KIND image/config ID | `sha256:24d95a2c6bc32eb224e73cce0ec1465c245c31853e7039046d2af82436760775`; tag `docker.io/grafana/alloy:v1.17.1`, repo digest matches the host index |
| linux/arm64 child manifest | `sha256:1f1917f970fb29b1135614235f3a02298e8d052a7b5dc90af35e95fa661cf5b9` |

Docker's tree shows one index with four platform descriptors. Only linux/arm64 has cached payload (685MB reported disk usage, 144MB content); amd64, ppc64le and s390x children each show 0B. KIND inspection confirms linux/arm64. Independent stores retain separate required references/copies; a root index, its platform manifest and its config ID are different levels of the same selected image, not duplicate versions. These reported sizes are not additive disk savings.

The complete supported host peer inventory contains five running, zero stopped/paused containers and no direct Alloy container. All 72 running/exited CRI containers and all 99 built-in workload/history objects were checked. Current and stopped Alloy consumers are:

- Running CRI container `3d376a0e47113222b9ef5cffd64409f762be4349b349f530afdc25bbb1202c97` and exited container `64fe576d76c8577d9cffc6b708b85a384653889fbeadd7af53ac2f87afb5648c`, both the retained config ID.
- Ready Pod `observability/alloy-2tm85`, UID `4b985ac8-c413-415d-966d-e0e164934b67`.
- DaemonSet `observability/alloy`, UID `743083e9-d7b4-4cd1-96bd-16ec9cb73566`, scheduled/Ready/available 1/1.
- ControllerRevision `observability/alloy-6598bb7569`, UID `39f4c34f-300d-4957-bbbc-1f2844b861c8`.

At 15:06:42 UTC, public registry reads SHA-256 verified the exact root index, selected arm64 manifest and config, with config matching the KIND ID. No layer payloads were fetched and no runtime repull or full recovery acceptance is claimed. Historical Helm image payload coverage remains limited by the permitted interface described above; no prohibited route was retried. This does not block the clear Alloy result: there is no older present candidate to retire, and the current image is in use.

Evidence: private `alloy-image-checkpoint.json` and `alloy-host-image-tree.txt` beside the live acceptance records. Removed IDs/tags: none; logical removed bytes 0; actual disk savings unmeasured.

### Subsequent approved reset and Alloy row cleanup

The later action-time authorization reset only Loki, Tempo, Prometheus and Alertmanager. Loki/Tempo now have fresh bound 10Gi standard claims; both old PVs and exact data directories are absent. Prometheus/Alertmanager have replacement Pod UIDs and their old emptyDir paths are absent. All four StatefulSets are ready 1/1 and their readiness endpoints pass; Doctor passes 7/7. No backup was created. The six protected claims and Grafana/Infisical Pod identities and images are unchanged.

Grafana reset was conditional on complete restoration evidence. All 29 dashboard UIDs match labeled ConfigMap JSON sources and report provisioned; all three data sources are read-only provisioned. Its PVC plugin directory is empty. Grafana was preserved because existing user/organization preferences cannot be verified through the anonymous API (401/403), and `grafana-admin` InfisicalSecret has `ReadyToSyncSecrets=False` since 12:06:55 UTC, before these resets. No credentials were retrieved and no identity/access changes were attempted.

The fresh Alloy check used Docker `desktop-linux` and `default`, which identify the same engine. Two displayed host rows share one root ID; the node has one required arm64 image. The tree contains one materialized arm64 child plus three zero-byte platform descriptors. The reported 15–20 UI rows are not reproduced by these supported inventories; the exact UI/store mismatch remains unverified.

Removing the unused digest-suffixed Alloy name unexpectedly caused Docker to remove both host names and the root object. The running/exited node copy was untouched. The required host image was restored by its same pinned public digest and platform, with only the active plain version tag. The recovery script now supports `--alloy-only` and the existing explicit `--node` option, without downloading the three retired images in that mode; it refuses to overwrite an existing different Alloy tag. Its live host/node `--verify-only` check passes. Final restoration evidence is recorded in private `alloy-current-inventory.json`; net physical images removed is zero, with one required image in each store.

Infisical upgrade, its preflight/backup, and SDK upgrade are dismissed from this task. They are not deferred execution items. The old Nova session and task worktree remain preserved, including unrelated untracked acceptance-helper sources and cache files.

## Duplicate build and pull prevention source acceptance

The human authorized project lifecycle rules and real deployment-helper changes in a separate source/code/test scope, with one isolated worker and independent review. Implementation commits `c03ac24992b57ad74041614b1256b07bba4c763b` and `0ea26ca8d8a34f7bc1e875f1917ec495982f06ad` were integrated as `85da2dffbece9670aa1494474de0c77598e96e67`. The reviewed source is used by the normal Host deployment helper; no runtime binary was installed and no live build, pull or cache receipt was created during this source acceptance.

The helper checks local immutable-tag collisions on every build path. Eligible opt-in reuse requires explicit platform, complete local source/dependency/configuration and effective argument fingerprints, verified builder/store/base evidence, and a private receipt matching the exact store image ID. Matching labels alone cannot adopt an existing image. Changed or unknown evidence and rebuild/refresh requests bypass reuse while preserving immutable-tag collision protection. Custom frontend selectors disable eligibility; eligible offline builds explicitly control all ten proxy arguments so implicit Docker client configuration cannot change the fingerprinted inputs. The current agent Dockerfile remains ineligible and uses the confirmed-absent fresh build path with base update checks.

KIND reuse is independent of build opt-in. Each required node must prove the exact digest, actual platform and complete content; absent, mismatching or unknown evidence takes the existing pull path and is reverified before Helm. Host Docker inventory cannot satisfy node requirements. Process-local locks serialize matching calls and recheck stores; independent CLI processes still require operator serialization. Registry collisions remain an operator gate. Arbitrary Helm/upstream pulls, host Docker upstream-pull caching and the migrated namespace's separate deployment script are outside these helper changes.

Independent review first identified exact-image receipt, frontend-selector and implicit-proxy blockers. The followup added focused regression coverage and resolved all three; fresh review found no new important issues. Root then verified the merged main checkout at 15:43 UTC: `GOPROXY=off GOSUMDB=off GOCACHE=/tmp/kuchdesk-resume-go-cache go test -count=1 ./...` from `host-agent` passed all packages, and `go test -race -count=1 ./internal/deploy` passed. Tests use fake image stores and temporary receipt directories. The full suite's existing local HTTP test servers used approved loopback sandbox escalation. Git whitespace checks passed. This proves source behavior, not live image reuse or disk savings.

[Supported profile fields and limits](../host-agent/README.md#avoiding-duplicate-builds-and-pulls) and [project rules](../AGENTS.md#avoid-duplicate-image-work) document the workflow. Private root verification logs are `host-tests-image-dedup-main.log` and `host-deploy-race-image-dedup-main.log` beside the live acceptance records. Image retirement remains blocked by the historical-reference and runtime-recovery gates above; removed IDs/tags and measured logical removed bytes remain zero.

## Bounded live image reuse acceptance

The human subsequently authorized building the merged Host binaries and bounded real-helper acceptance. This used actual `Workflow` helpers and `ExecRunner`. Full Helm apply remains outside this test: the generic CLI lacks the required renderer for the migrated agent release. The Host bridge remained PID 1389 at its existing executable path; no service restart, launchd, credential, trust or permission setting changed.

Two tiny offline `scratch` images were built from clean fixture source commits using the existing source-tag convention. Three concurrent initial callers performed exactly one build; unchanged inputs reused its exact store ID. Changing payload, configuration and an explicit argument rejected the old immutable tag and built exactly one new source-tagged image. Concurrent callers and a repeat added no duplicate build. Saved image archives verified `/payload` and effective argument labels. Both images, source revisions and private receipts remain.

| Fixture | Source commit | Host store image ID |
|---|---|---|
| Initial | `9eac94722bfb5b06c5d03ad121968cf2d6047aab` | `sha256:bd91a67bf972ebdac6e15a9e919e443e956b493c29a3f5b770167f0b2f99ff4b` |
| Changed | `98a42f1da36b5008e16ec15b113aa169b9033299` | `sha256:08f99c78d09789c38399285e57cb076bd499af751f7a9ce71775a9ea2bf311a6` |

These are local-only Host fixtures, linux/arm64; they require no registry publication or node copy. The separate required KIND store target was the unchanged deployed agent `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75` on `desktop-control-plane`, also linux/arm64. Its config ID remained `sha256:47db29c7d13f3e5f53afa5647d92209d1cc55b475a99b01977164d623d1dc696`.

The initial node test failed safely: the registered/unpacked image was incomplete (2/3 blobs). A normal transfer pull and CRI registration succeeded but left compressed layer `sha256:d5def5e744d2c42b607f368cccd0da74fcf7a58c17514aeaf670cc8f2b62c43c` (126,010 bytes) absent. This was missing content, not a digest/platform mismatch. Containerd's [transfer unpacker](https://raw.githubusercontent.com/containerd/containerd/v2.3.1/core/unpack/unpacker.go) can skip a blob when its snapshot exists. A regression reproduced failure before the fix. The helper now retains the normal transfer pull and its verifier policy, checks the exact reference, and performs a guarded [content fetch](https://raw.githubusercontent.com/containerd/containerd/v2.3.1/cmd/ctr/commands/content/fetch.go) only if content remains incomplete. CRI registration and final digest/platform/content verification still run; no global runtime/security settings change.

At 16:04:24 UTC resumed live acceptance passed under the race detector with every new build forbidden. Three Host callers reused the retained changed fixture. Three node callers shared one normal pull, one necessary content fetch and one CRI registration; final content was complete and later calls performed no further operation. The initial failed attempt and corrective attempt are separate evidence, not one successful pull across the investigation. Full Host and deploy race suites passed after the source fix; default tests skip the two explicitly opt-in live tests.

Fresh health: Doctor at 16:09:16 UTC passed 7/7 without warnings. At 16:12:32 UTC all 17 scoped Pod UIDs/images remained unchanged and Ready, node Ready, all eight captured PVCs and captured Ingress/TLSStore UIDs/specs unchanged. Grafana/Infisical DNS remained 192.168.1.100; verified HTTPS returned 200 and leaf fingerprints stayed unchanged. An earlier Doctor attempt used a nonexistent fixture domain and failed only that DNS input. Python's separate CA store lacked the local issuer; system curl verified using the existing macOS trust path. No trust setting changed.

Private `~/.codex/task-artifacts/kuchdesk-image-reuse-live-20261011/` retains the Host/Deploy/MCP/Doctor binaries, scratch sources/receipts, `live-test-initial.log`, `live-test.log`, `live-acceptance-repair.json`, `host-tests-followup.log`, `doctor-after.json` and `health-after.json`. Binary installation or registered-runtime activation is not claimed by this helper test. No image reference/content identity, Pod, release, persistent storage or access setup was replaced. No images were removed; disk savings remain unmeasured.

The [exact 31 historical revision pairs and smallest safe evidence procedure](historical-image-reference-export.md) are recorded without another history investigation. No permitted image-only exporter is currently available, so no full-manifest pipeline is provided or executed and every retirement candidate remains retained.

## Later authorized image discard and fresh-reset proposal

The user subsequently explicitly accepted loss of the three old Blackbox/k6
rollback images and asked for one download script. This supersedes the earlier
historical rollback-retention blocker for those exact images only. No denied
Helm/Secret query or history deletion was used. Current checks covered five Host
containers, 72 running/exited CRI containers, 84 current workload objects including
zero-replica templates, and an empty k6 TestRun/PrivateLoadZone inventory; no
candidate consumer was found. All three exact registry root manifests were
SHA-256 verified and their selected platform graphs passed remote fast validation
without downloading layers. This is availability evidence, not a full runtime
repull. The user accepted the old rollback-image loss.

Normal `docker image rm --no-prune` removed Host roots `a50c4c0eda29…`,
`e66db15b8601…` and `b97b8e32867a…`. Normal CRI removal deleted selected node
configs `339431e78365…`, `4d79f9384ce4…` and `67876cb51a8e…` respectively.
Full identities, references and operation receipts are retained in private
`cleanup-current-images.json` and `cleanup-actions.json` in the
`kuchdesk-image-reuse-live-20261011` task artifact directory. No force/global prune,
volume, application-data or history deletion occurred. Host image count fell
40 to 37, with every other Host image ID retained; node image count became 63.
The old sidecar and current Alloy image remain preserved.

Docker's image-store layer counter fell by **61,162,523 bytes**. Node containerd
allocated usage fell by **130,056 KiB**. These are separate measurements, not
additive physical Mac disk savings; asynchronous GC/background writes can affect
the node measurement and virtual-disk shrinkage was not measured. Logical image
bytes removed were 65,340,957 in Host Docker and 65,345,592 in the node store.

At 17:17:06 UTC Doctor passed 7/7, all 33 current Pod UIDs/images remained
unchanged and Ready, and all eight PVC UIDs/bindings remained unchanged.
Infisical, its PostgreSQL/Redis, environments, secrets, identities/access and
availability were preserved. The user excluded them from a proposed fresh reset.
The concrete private `fresh-reset-manifest.json` proposes Loki/Tempo data claims,
Prometheus/Alertmanager emptyDir data, and an explicitly optional Grafana database
reset. It awaits target-specific permanent-data confirmation; no reset or new
backup/encryption was executed. No separate external-project PostgreSQL/Redis
instance was identified in the current Docker/Kubernetes inventory; unidentified
remote/other-context databases are excluded.

[Download script](../lab/scripts/download-retired-images.py): one standalone
Python 3 file, Docker only by default, with optional explicitly selected KIND
node restoration/verification. Source safety tests and real Host verification
passed without a recovery download. The old Blackbox node cache was already
incomplete; verification refused it rather than claiming completeness. The
script's guarded repair path has focused regression coverage. Do not run it as
part of a fresh start unless restoration of these old images is intended.
