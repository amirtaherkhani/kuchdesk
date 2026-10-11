# Approval-ready old agent image discard — 2026-10-11

**Proposal only: permanently discard exactly eight old host Docker builds. No deletion performed.** This is an explicit discard decision, not a claim that missing index/attestation content is recoverable. No backup is required or proposed unless requested.

Fresh reference checks: **2026-10-11 06:28:09 UTC**, Docker `desktop-linux`, Kubernetes `docker-desktop`. Checked 84 all-namespace built-in workload objects (including zero replicas and init/ephemeral image fields), 75 running/exited node containers and all 5 Docker runtime containers via `/system/df`. No active/current desired, zero-replica, running or stopped-container reference to these eight host objects/tags or the three separately listed old node configs was found. Historical Helm references **do exist** and are listed below; they are not current workloads.

## Exact host-only approval set

Sizes are Docker accounting at this snapshot. Unique bytes = object size minus shared bytes; these columns are **not additive predictions of reclaimed storage**. Build cache, alias/index graphs and shared layers can retain bytes after an image object is discarded. Measure actual store counters before/after a later approved action.

| Tag | Full Docker object ID | Logical bytes | Shared bytes | Unique bytes | Current container references |
|---|---|---:|---:|---:|---:|
| `kuchdesk-agent:sha-88df02f` | `sha256:f14a96960f952c1c164f7abd76ef458a6ec7f7ff083316a056d0b73df595ffbb` | 35942386 | 363580 | 35578806 | 0 |
| `localhost:5001/kuchdesk-agent:git-05eb7d8778ec` | `sha256:35581d2e4d5d24382776ee7f32c761e61160030d95c6ba1c3a54ebd8d032fa87` | 36029262 | 36026659 | 2603 | 0 |
| `localhost:5001/kuchdesk-agent:git-2207976ed703` | `sha256:8f3c0ce0bccce04a6fe71bf2552aeb3f45fcc309259dd8f2b6b1ef427443c5fb` | 36029262 | 36026659 | 2603 | 0 |
| `localhost:5001/kuchdesk-agent:git-6518cb27201e` | `sha256:53d01fde43785de9886d41cce4f0681803bd59ccb78de07cfa773eb3add872e8` | 36027404 | 36024801 | 2603 | 0 |
| `localhost:5001/kuchdesk-agent:git-8ef70478ac83` | `sha256:fb983b1cbe9079de4a3989bab593bdbf74f44ae515305768d9fa88b48c1b1d1e` | 36025904 | 36023301 | 2603 | 0 |
| `localhost:5001/kuchdesk-agent:git-93a8dfc80966` | `sha256:cac459efd3b338094124a6d3b414973c8100fa3b27ed65efdb0303b15cca980f` | 36027404 | 36024801 | 2603 | 0 |
| `localhost:5001/kuchdesk-agent:git-e7fed2157a06` | `sha256:d32870cd6f9889455784bb70a7a338f2d45e6dd6a3aa6ef646c159733831e923` | 36025901 | 36023301 | 2600 | 0 |
| `localhost:5001/kuchdesk-agent:git-fb634fe` | `sha256:7b6ec0fc656f46b43455bff9892266f7d801b98405f24dc418c65aa95c5962a1` | 35943434 | 363580 | 35579854 | 0 |

## Current image kept

- Host Docker index: `sha256:97b776d36152d1b60d293cb67f6df7a6e093eee5f4e04aeea102bf2d0ab7fff1`, tag `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a`.
- Current node platform manifest: `sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75`.
- Current node config: `sha256:47db29c7d13f3e5f53afa5647d92209d1cc55b475a99b01977164d623d1dc696`, including its running and exited container references.
- Current workload is `platform-tools/kuchdesk-agent`. Helm release history is still discoverable as `development-tools/kuchdesk-agent` revision 7; this report does not change release metadata or namespaces.

## Actual Helm rollback references

Read `helm get manifest` for all seven retained revisions in memory; parsed YAML/JSON documents semantically and extracted only Deployment container image fields. Rechecked 2026-10-11 after correcting a text-only parser that missed revision 7’s JSON-form YAML. No Kubernetes Secret payload query or release-secret decoding was used. These references are historical rollback inputs, not a promise that old chart namespaces/configuration remain usable.

| Revision | Status | Rendered image references |
|---|---|---|
| 1 | superseded | `localhost:5001/kuchdesk-agent:git-8ef70478ac83@sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b` |
| 2 | superseded | `localhost:5001/kuchdesk-agent:git-6518cb27201e@sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45` |
| 3 | superseded | `localhost:5001/kuchdesk-agent:git-93a8dfc80966@sha256:803f5cc661a89672329038387487ce4b290ab5030c470c8080d7d12aa7727d45` |
| 4 | superseded | `localhost:5001/kuchdesk-agent:git-05eb7d8778ec@sha256:a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f` |
| 5 | superseded | `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75` |
| 6 | superseded | `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75` |
| 7 | deployed | `localhost:5001/kuchdesk-agent:git-8c1d9ab3de4a@sha256:348b7e44330c2a9c7a5fa108e885a6a8d0b0da1e1e73d78802fe05be09c28b75` |

**Rollback consequence:** where a historical revision names an old tag/platform digest, discarding the host index removes that local exact build/index source. Existing registry platform manifests and retained node payloads can still supply some historical runnable payloads today, but they do not restore a missing original host OCI index or attestation graph. If registry/node copies later disappear, historical rollback may fail. Do not describe the old images as “unreferenced everywhere.”

## Irreversible-loss acknowledgement

Earlier exact-digest registry checks returned HTTP 404 for all eight old Docker index IDs above. Tag-based HEAD responses returned different platform-manifest digests. Complete byte checks verified several historical runnable node payloads, **not** those missing indices/attestations. The local-only `kuchdesk-agent:sha-88df02f` has no independently verified recovery source.

The requested decision is therefore permission to **permanently lose these exact eight local build/index objects and any unique missing index, attestation or local-only content**, accepting the historical rollback-source consequence above. Rebuilding from a commit-looking tag is not guaranteed byte-identical recovery. The registry, remaining node images and Git source are not being deleted in this host-only scope.

## Optional node scope — excluded from host-only approval

These three old node configs have no retained running/exited container or current desired/zero-replica reference in the fresh scan. They retain some historical runnable payload/alias sources; their optional discard would further reduce rollback options. **Do not remove them under a host-only “yes.”** A combined approval must explicitly name this additional three-config set and accept loss of its original index aliases, even though the platform payload bytes were previously verified from the existing registry.

| Optional full node config ID | Exact current aliases |
|---|---|
| `sha256:8db48db0a69c465446c66409324452505d057177e9f62820da23d1375ef7de20` | `localhost:5001/kuchdesk-agent:git-2207976ed703`<br>`localhost:5001/kuchdesk-agent:git-05eb7d8778ec@sha256:a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f`<br>`localhost:5001/kuchdesk-agent@sha256:8f3c0ce0bccce04a6fe71bf2552aeb3f45fcc309259dd8f2b6b1ef427443c5fb`<br>`localhost:5001/kuchdesk-agent@sha256:a557a1c0cfab875ad1be6b744083a5aba5cbd642405cc4a72df9e03cb907c65f` |
| `sha256:ed99259af2eed7786d10e5bf5858c3e533bef4cfc55519c9ee71ee5bf5e9d82b` | `localhost:5001/kuchdesk-agent:git-8ef70478ac83`<br>`localhost:5001/kuchdesk-agent:git-e7fed2157a06`<br>`localhost:5001/kuchdesk-agent:git-8ef70478ac83@sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b`<br>`localhost:5001/kuchdesk-agent:git-e7fed2157a06@sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b`<br>`localhost:5001/kuchdesk-agent@sha256:11da87f60ddfee850d1d1ab548d73ae1d695b9ff63346accdb863badb9c7fa5b`<br>`localhost:5001/kuchdesk-agent@sha256:fb983b1cbe9079de4a3989bab593bdbf74f44ae515305768d9fa88b48c1b1d1e` |
| `sha256:ea564bdeb8a8caabfa30aaf3a9aa0603bc8cb57c5fe1676742858ffdc546ef94` | `localhost:5001/kuchdesk-agent:git-fb634fe`<br>`localhost:5001/kuchdesk-agent@sha256:7b6ec0fc656f46b43455bff9892266f7d801b98405f24dc418c65aa95c5962a1` |

## Concrete confirmation wording

> Permanently discard only the eight old **host Docker** KuchDesk-agent image objects listed above, keeping the current `97b776d3…` image and all node images. I accept loss of unrecoverable original indices/attestations/local-only content and the historical rollback-source impact. Do not create a backup.

A combined host-and-node request must additionally state: “Also discard the three optional old node config objects and their exact aliases listed above; keep current node config `47db29c7…` and every container.” Do not infer this extension from a host-only approval.

Immediately before any later action, recheck context, full IDs, alias sets, current/protected images and all retained/desired references; stop if a new active reference or mismatch appears. Use supported non-force exact-object removals only. Any unexpected dependency refusal is a stop, not permission to force or broaden.

**Always excluded:** current builds, all unrelated/service/system/node-runtime images, BuildKit cache pruning, containers (including exited), volumes, PVC/PVs, registry garbage collection, credentials, Helm revisions and runtime upgrades. Offline projects, external tools, future operator jobs and manual consumers remain detection limits. The separate full-service inventory does not expand this exact discard scope. This proposal reclaimed 0 bytes.
