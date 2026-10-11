# Local image inventory — 2026-10-11

**Bounded cleanup complete in both stores, with retained items. No cleanup operation remains running.** One specifically approved obsolete node image was removed; Docker removed none. Eight old Docker objects and three old node objects remain because exact original index recovery is unproved. All current/shared/system objects remain protected. Initial health/recovery gates and inventory checkpoints below are historical; the final coordinated action records actual removal and per-store allocation counters.

Snapshot: 2026-10-11 05:56:12 UTC. Docker context `desktop-linux`; Kubernetes context `docker-desktop`; existing node container `desktop-control-plane`, ID `ba5c6a0bc09a679b59240d349082650c379abeab7813cd27e6928031550ba337`, image `kindest/node:v1.36.1`, Docker ID `sha256:3489c7674813ba5d8b1a9977baea8a6e553784dab7b84759d1014dbd78f7ebd5`. Node runtime is containerd; its only listed namespace is `k8s.io`.

## Distinct stores and names

Host Docker and node containerd are separate stores. The node has a Docker volume mounted at `/var`; node image caches are inside that volume. Do not count its entire size as reclaimable image data and do not delete the volume. Image index/manifest digests and CRI config IDs identify different objects; identical tag text across stores does not mean identical identity or disk accounting.

Docker returned 47 image objects. Multiple tags/digest aliases on the same object are not physical duplicates: for example `grafana/alloy:latest` and `grafana/alloy:v1.20.1` share Docker ID `sha256:2aa2099af76c0098d4af7a4d6e48f86cb66dc1a000222ad927a1c67c6542d13f`. Removing one alias does not reclaim an independently sized image. Separate image objects can still share almost every layer.

## Exact project Docker objects

These are **potential review candidates**, not approved removals. Preserve the current `git-8c1d9ab3de4a` object. The remaining eight old project objects have zero Docker container references in `/system/df` and no corresponding old desired Kubernetes image was found. Recovery and health gates still apply.

| Docker object ID | Tags | Logical size bytes | Shared bytes | Unique bytes (size minus shared) | Containers |
|---|---|---:|---:|---:|---:|
| `sha256:f14a96960f952c1c164f7abd76ef458a6ec7f7ff083316a056d0b73df595ffbb` | `kuchdesk-agent:sha-88df02f` | 35942386 | 363580 | 35578806 | 0 |
| `sha256:35581d2e4d5d24382776ee7f32c761e61160030d95c6ba1c3a54ebd8d032fa87` | `localhost:5001/kuchdesk-agent:git-05eb7d8778ec` | 36029262 | 36026659 | 2603 | 0 |
| `sha256:8f3c0ce0bccce04a6fe71bf2552aeb3f45fcc309259dd8f2b6b1ef427443c5fb` | `localhost:5001/kuchdesk-agent:git-2207976ed703` | 36029262 | 36026659 | 2603 | 0 |
| `sha256:53d01fde43785de9886d41cce4f0681803bd59ccb78de07cfa773eb3add872e8` | `localhost:5001/kuchdesk-agent:git-6518cb27201e` | 36027404 | 36024801 | 2603 | 0 |
| `sha256:97b776d36152d1b60d293cb67f6df7a6e093eee5f4e04aeea102bf2d0ab7fff1` | `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a` | 36843343 | 363580 | 36479763 | 0 |
| `sha256:fb983b1cbe9079de4a3989bab593bdbf74f44ae515305768d9fa88b48c1b1d1e` | `localhost:5001/kuchdesk-agent:git-8ef70478ac83` | 36025904 | 36023301 | 2603 | 0 |
| `sha256:cac459efd3b338094124a6d3b414973c8100fa3b27ed65efdb0303b15cca980f` | `localhost:5001/kuchdesk-agent:git-93a8dfc80966` | 36027404 | 36024801 | 2603 | 0 |
| `sha256:d32870cd6f9889455784bb70a7a338f2d45e6dd6a3aa6ef646c159733831e923` | `localhost:5001/kuchdesk-agent:git-e7fed2157a06` | 36025901 | 36023301 | 2600 | 0 |
| `sha256:7b6ec0fc656f46b43455bff9892266f7d801b98405f24dc418c65aa95c5962a1` | `localhost:5001/kuchdesk-agent:git-fb634fe` | 35943434 | 363580 | 35579854 | 0 |

Docker repo digests for these objects match the Docker IDs printed above (`localhost:5001/kuchdesk-agent@<ID>`, except the local `kuchdesk-agent:sha-88df02f` repository). They are OCI index identities, not the CRI config IDs below. Do not issue deletions using a truncated ID.

## Exact project node objects

All node containers were inventoried with `crictl ps -a`, including exited containers. CRI image aliases are grouped by config ID. The current object is referenced by one running and one exited container; the other four have no matching retained CRI container reference. `pinned=false` is not deletion authorization.

| CRI config ID | Stored tag/digest aliases | CRI size bytes | Retained container references |
|---|---|---:|---|
| `sha256:8db48db0a69c465446c66409324452505d057177e9f62820da23d1375ef7de20` | `localhost:5001/kuchdesk-agent:git-2207976ed703`<br>`localhost:5001/kuchdesk-agent:git-05eb7d8778ec@sha256:a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f`<br>`localhost:5001/kuchdesk-agent@sha256:8f3c0ce0bccce04a6fe71bf2552aeb3f45fcc309259dd8f2b6b1ef427443c5fb`<br>`localhost:5001/kuchdesk-agent@sha256:a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f` | 8583376 | None found |
| `sha256:47db29c7d13f3e5f53afa5647d92209d1cc55b475a99b01977164d623d1dc696` | `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75`<br>`localhost:5001/kuchdesk-agent@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75` | 8807633 | b250a5c508b9a25d3c1ca4bac9198fbdabf4d26c48ea4c31b37ca6ec011f6aaa (CONTAINER_RUNNING)<br>84b00fbf011819564ec07d8239ad7face91dbd7b49e05b8685c4ad807b0ed3ab (CONTAINER_EXITED) |
| `sha256:ed99259af2eed7786d10e5bf5858c3e533bef4cfc55519c9ee71ee5bf5e9d82b` | `localhost:5001/kuchdesk-agent:git-8ef70478ac83`<br>`localhost:5001/kuchdesk-agent:git-e7fed2157a06`<br>`localhost:5001/kuchdesk-agent:git-8ef70478ac83@sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b`<br>`localhost:5001/kuchdesk-agent:git-e7fed2157a06@sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b`<br>`localhost:5001/kuchdesk-agent@sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b`<br>`localhost:5001/kuchdesk-agent@sha256:fb983b1cbe9079de4a3989bab593bdbf74f44ae515305768d9fa88b48c1b1d1e` | 8580954 | None found |
| `sha256:ea564bdeb8a8caabfa30aaf3a9aa0603bc8cb57c5fe1676742858ffdc546ef94` | `localhost:5001/kuchdesk-agent:git-fb634fe`<br>`localhost:5001/kuchdesk-agent@sha256:7b6ec0fc656f46b43455bff9892266f7d801b98405f24dc418c65aa95c5962a1` | 8564025 | None found |
| `sha256:cb1cd3cbc4f9b13a2e12c576c46f8ae6197dfae37a6d88ea52c1dca5209d8c1b` | `localhost:5001/kuchdesk-agent:git-6518cb27201e@sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45`<br>`localhost:5001/kuchdesk-agent@sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45` | 8581518 | None found |

The `git-8ef70478ac83` and `git-e7fed2157a06` aliases resolve to one node config object; the `git-05eb7d8778ec` and `git-2207976ed703` lineage also shares one config object. The node current manifest digest is `sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75`, config `sha256:47db29c7d13f3e5f53afa5647d92209d1cc55b475a99b01977164d623d1dc696`. Both are protected, along with host Docker index `sha256:97b776d36152d1b60d293cb67f6df7a6e093eee5f4e04aeea102bf2d0ab7fff1`.

## Desired and retained consumers

Inspected 84 Kubernetes Pods/Deployments/ReplicaSets/StatefulSets/DaemonSets/ReplicationControllers/Jobs/CronJobs across all namespaces, including zero replicas, and 75 node CRI containers including exited ones. Project image references:

- `Pod platform-tools/kuchdesk-agent-75d88bc94-lncpf` (replicas not applicable): `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75`
- `Deployment platform-tools/kuchdesk-agent` (replicas 1): `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75`
- `ReplicaSet platform-tools/kuchdesk-agent-75d88bc94` (replicas 1): `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75`

Docker `ps -a` omitted internal containers, but read-only `/system/df` exposed all five; no stopped Docker container was returned by that inventory. These are protected:

- `aecb18726452930daa126008e60e09244790e9f9c2cc1075f14fa5cb04588511`: /kindccm-73LHK2O2CRYVXUEFGD77Y3CDVC6MBI33G2QBHPUQ — `envoyproxy/envoy:v1.36.7`, running
- `b79d07fb8125672b1f1e4ac617521fddd2bd6bbd17386d3f9dcae87f38cb3df2`: /kindccm-KXXUW6KBH5Z6GR4ATQG7DBLSTUVKU4CSMCQOPVEH — `envoyproxy/envoy:v1.36.7`, running
- `9d12a48af63c2d15f7b0b526203500597b34847a8618149e60dce352c1c848dd`: /kind-cloud-provider — `docker.io/docker/desktop-cloud-provider-kind:v0.7.0`, running
- `307fb2b51dadabcdb169654976fa4bb2fbcbf5380a640958e8bcf80c9672cd72`: /kind-registry-mirror — `docker.io/docker/desktop-containerd-registry-mirror:v0.0.4`, running
- `ba5c6a0bc09a679b59240d349082650c379abeab7813cd27e6928031550ba337`: /desktop-control-plane — `kindest/node:v1.36.1`, running

Zero-replica non-project examples include old blackbox-exporter digest `sha256:a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d`; they are retained dependencies, not unused merely because replicas are zero. All node/system/shared/unrelated images, containers, volumes and build caches are outside this cleanup lane. Helm rollback revisions, offline manifests and external automation may still refer to historical project images; absence from current desired objects is not recovery proof.

## Recovery evidence

Registry manifest HEAD checks were read-only, without login, pull or blob download:

| Tag | HEAD result | Returned digest or failure class |
|---|---|---|
| `git-8c1d9ab3de4a` | `200` | `sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75` |
| `git-05eb7d8778ec` | `200` | `sha256:a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f` |
| `git-2207976ed703` | `200` | `sha256:a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f` |
| `git-6518cb27201e` | `200` | `sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45` |
| `git-93a8dfc80966` | `200` | `sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45` |
| `git-8ef70478ac83` | `200` | `sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b` |
| `git-e7fed2157a06` | `200` | `sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b` |
| `git-fb634fe` | `200` | `sha256:b260728900b58a31a93525b5cbe2682a2ed52c72d8061f1e0f8e1bdeb5d0e2c3` |

A successful manifest HEAD only proves metadata is served now; it does not verify every config/layer blob, export archive, replayable build inputs, or a tested restore. A local copy in the other store is not an independent recovery source if both stores will be cleaned. At the initial HEAD-only checkpoint, no candidate had fully verified independent recovery. See the subsequent byte-level recovery evidence below. Public registry tags, commit-looking tags and retained Git history are not sufficient proof of exact byte-for-byte recoverability.

## Space accounting

- Docker `/system/df` reports `LayersSize=7524685319` bytes for its image store; this is the baseline store-level counter, not the sum of each image row.
- Human-readable Docker totals: images 7.525 GB, globally “reclaimable” 5.042 GB; build cache 5.949 GB; volumes 20.43 GB. These broad estimates include protected/unrelated resources and are **not** scoped cleanup targets.
- Node allocated directory baseline (`du -sk`): content store 929,920 KiB = 952,238,080 bytes; overlayfs snapshot store 18,596,984 KiB = 19,043,311,616 bytes (use KiB counter as authoritative; overlayfs includes active container data). Neither baseline is a reclaim estimate for the four project objects.
- Project Docker paired historical objects each show only about 2.6 kB unique in isolation despite approximately 36 MB logical size. Shared layers and BuildKit references can prevent reclamation. CRI sizes are compressed content sizes and cannot be added to Docker logical sizes to claim savings.
- **At the initial inventory checkpoint, reclaimed bytes were 0.** See the final measured cleanup result below. Immediately before any later scoped deletion, capture the same store counters, exact object/ref lists and node content/snapshot directory allocation. Recheck after garbage collection settles and report per-store observed deltas, noting concurrent writes. Do not label tag count, summed logical sizes or a prediction as measured reclaimed space. Never double-count node caches inside the Docker volume.

## Gate before any later removal

The root lane must confirm tests, full health (including enrollment), exact protected running/rollback images and independently verified recovery. Recheck all retained containers and desired/zero-replica images at action time. Coordinate the exact eight Docker objects and four node config objects with the root lane before any removal; this report does not approve that entire candidate set. No force deletion, broad prune, rebuilds, runtime migration, registry garbage collection or volume removal was performed. A later explicitly authorized pinned recovery pull is recorded below.


## Recovery follow-up — byte validation, before cleanup

The root lane reports fresh target API/auth/SSE/metrics, Host bridge project list (HTTP 200, two projects), and Doctor 8/8 with enrollment ready. No grant or login mutation was needed. Integrated test completion and final coordination remain the root lane's responsibility.

**All eight old Docker index digests returned HTTP 404** from `localhost:5001/v2/kuchdesk-agent/manifests/<exact-index-digest>`. Tags returning HTTP 200 pointed to platform manifests, not those exact OCI indices. Preserve all eight host Docker objects, including local-only `sha-88df02f`: this lane has not proved exact index/attestation recovery from the registry. No new durable export backup was created.

For the four historical node config objects, the following exact platform manifests were fetched by digest. Each manifest's SHA-256 was checked; each referenced config and layer was streamed completely from the existing registry, with SHA-256 and byte count checked. No downloaded file was saved. This establishes retrievable complete payload bytes, stronger than HEAD; it does not recover missing index metadata.

| Registry platform manifest SHA-256 | Expected verified CRI config SHA-256 | Layer payload bytes excluding shared base/config | Exact node-reference recovery |
|---|---|---:|---|
| `a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f` | `8db48db0a69c465446c66409324452505d057177e9f62820da23d1375ef7de20` | 8,455,542 | Payload verified; existing index alias is unavailable from registry |
| `803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45` | `cb1cd3cbc4f9b13a2e12c576c46f8ae6197dfae37a6d88ea52c1dca5209d8c1b` | 8,453,684 | Both existing digest aliases reference this verified manifest |
| `11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b` | `ed99259af2eed7786d10e5bf5858c3e533bef4cfc55519c9ee71ee5bf5e9d82b` | 8,452,184 | Payload verified; existing index alias is unavailable from registry |
| `b260728900b58a31a93525b5cbe2682a2ed52c72d8061f1e0f8e1bdeb5d0e2c3` | `ea564bdeb8a8caabfa30aaf3a9aa0603bc8cb57c5fe1676742858ffdc546ef94` | 8,435,255 | Payload verified; existing index alias is unavailable from registry |

Each config is 1,233 bytes. All four share the 126,010-byte compressed base layer `sha256:d5def5e744d2c42b607f368cccd0da74fcf7a58c17514aeaf670cc8f2b62c43c`; that shared layer is not exclusive reclaimable space.

A supported pinned runtime pull was then tested using the existing node: `crictl pull localhost:5001/kuchdesk-agent@sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45`. It succeeded and returned config `sha256:cb1cd3cbc4f9b13a2e12c576c46f8ae6197dfae37a6d88ea52c1dca5209d8c1b` as up to date. The existing cache made this a cached runtime pull; complete registry byte verification above supplies the independent missing network evidence. No test workload or container was started.

**Conservative removal proposal for root review: one node CRI object only**, `sha256:cb1cd3cbc4f9b13a2e12c576c46f8ae6197dfae37a6d88ea52c1dca5209d8c1b`, with its two existing `803f5cc6…` aliases. No retained CRI container or current/zero-replica Kubernetes object references it. Preserve all current objects and all host Docker objects. The other three node payloads are recoverable as platform images, but complete original index-reference recovery is unproved; do not silently equate those two guarantees. No removal had occurred at this recovery checkpoint. The later exact coordinated removal and refreshed baselines follow.


## Final coordinated removal — 2026-10-11 06:03–06:04 UTC

Root review approved **only** node CRI config `sha256:cb1cd3cbc4f9b13a2e12c576c46f8ae6197dfae37a6d88ea52c1dca5209d8c1b` under the user's scoped cleanup authorization. Immediately before action, this lane rechecked all 84 desired Kubernetes objects and 75 retained node containers, confirmed no references to the config, either historical tag or manifest digest, confirmed the current image remained present, and verified the candidate had exactly these two aliases:

- `localhost:5001/kuchdesk-agent:git-6518cb27201e@sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45`
- `localhost:5001/kuchdesk-agent@sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45`

Supported non-force `crictl rmi` was invoked for that full config ID only. It succeeded and reported both aliases deleted. The config no longer appears in CRI image inventory. No manual content/snapshot deletion, force option, prune or registry garbage collection was used.

| Store/counter | Immediately before | After normal GC | Observed decrease |
|---|---:|---:|---:|
| Host Docker image `LayersSize` | 7,524,685,319 bytes | 7,524,685,319 bytes | **0 bytes** |
| Node containerd content allocation | 929,920 KiB | 921,656 KiB | **8,462,336 bytes** |
| Node overlayfs snapshot allocation | 18,596,984 KiB | 18,570,408 KiB | **27,213,824 bytes** |
| Node combined measured allocation | 19,995,549,696 bytes | 19,959,873,536 bytes | **35,676,160 bytes (34.02 MiB)** |

Two successive short post-removal checks returned identical node counters. These are observed allocated-directory changes, not logical image-size sums. The cluster was running, so concurrent writes/normal garbage collection can affect attribution; this does not prove the same reduction in the macOS sparse VM disk's physical allocation. Do not double-count this node decrease as Docker-volume reclamation.

**Preserved and verified:** all nine project Docker objects (the eight old indices plus current), the three other old node config objects, and current node config `47db29c7…`. Its existing running container `b250a5c5…` and exited container `84b00fbf…` remained. System/shared/unrelated images, containers, volumes, registries and PVCs were untouched. The root lane completed the final application/Host/Doctor health recheck at 06:05 UTC: health and readiness HTTP 200; authenticated snapshot HTTP 200 and unauthenticated HTTP 401; SSE connection/reconnection passed; project list HTTP 200 with two projects; Doctor 8/8 with enrollment ready; metrics available. Targeted Go API/bridge tests passed. This health record is supplied by the root lane. Policy commit: `6da40e0`. No further cleanup is authorized by this report.
