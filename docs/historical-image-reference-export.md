# Historical image reference evidence

Image retirement remains blocked. Automatic approval review rejected historical
full-manifest retrieval because raw release output can contain Secret objects.
No safe permitted image-only exporter is currently exposed in this session, and
Helm has no built-in image-only equivalent covering manifests and hooks. Do not
retry `helm get manifest`, `get hooks`, `get values`, `get all`, or direct Helm
Secret decoding through a filtering wrapper in the assistant. No raw release
payload or Secret data is needed in the returned evidence.

## Exact retained scope

The existing read-only metadata checkpoint was taken at 2026-10-11 15:08:41 UTC
in context `docker-desktop`. These are **31 retained revision pairs**, using the
Helm metadata namespace rather than a migrated workload namespace:

| Helm namespace | Release | Exact retained revisions |
|---|---|---|
| observability | alloy | 1 |
| platform-system | cert-manager | 1 |
| observability | grafana | 1, 2, 3, 4 |
| platform-secrets | infisical | 1 |
| platform-secrets | infisical-operator | 1, 2, 3, 4 |
| observability-tests | k6-operator | 1, 2 |
| development-tools | kuchdesk-agent | 1, 2, 3, 4, 5, 6, 7 |
| observability | loki | 1, 2, 3, 4 |
| kube-system | metrics-server | 1, 2 |
| observability | monitoring | 1, 2 |
| observability | tempo | 1 |
| platform-system | traefik | 1, 2 |

Chart versions, statuses and revision timestamps are retained in the private
`~/.codex/task-artifacts/kuchdesk-approved-loki-traefik-20261011/historical-image-evidence-required.json`.
Do not expand the scope into another broad history investigation. Do not discard
revisions or rewrite rollback resources to make an image appear unused.

## Smallest safe user procedure

1. Open the exact scope JSON above. Use an existing trusted **image-only**
   exporter or an operator-reviewed attestation interface for these 31 pairs.
   There is no safe executable export command available to provide here; do not
   substitute a full-manifest/Secret retrieval pipeline.
2. Return one JSON evidence file with its context, UTC observation time,
   matching release/revision metadata, and only image references with their
   resource kind/name and field path. Cover containers, initContainers, hooks,
   and embedded workloads. Record a successful empty image list explicitly for
   any revision with no images; an error or omitted revision is not emptiness.
3. Include exact immutable identities and all recorded tags. Flag unresolved
   mutable references or unexamined embedded workloads; they prevent an absence
   attestation. A current registry resolution of an old mutable tag does not
   establish its historical content. Attest whether the three candidates below
   occur anywhere in the complete covered scope. Omit Secret bodies, encoded
   release payloads, credential values and raw manifests.

If no trusted interface exists, **stop and retain every candidate**. This document
does not authorize provisioning an exporter, creating access or retrieving
rejected payloads. The smallest immediate action is opening the scope file;
an approved image-only interface or trusted operator attestation is still needed.

| Candidate | Exact root digest |
|---|---|
| Old Blackbox | `sha256:a50c4c0eda297baa1678cd4dc4712a67fdea713b832d43ce7fcc5f9bea05094d` |
| Alternate k6 runner | `sha256:e66db15b860113878fa74670e31f5e274830b7b6e42c8bff28b2f2d86a257603` |
| Alternate k6 starter | `sha256:b97b8e32867a553b7f866acd1e0a2ac91af81480c04c12a6ee55df3ae99d2ad6` |

The old sidecar is separately protected by a known ControllerRevision reference;
it is not an unreferenced retirement candidate. Historical image evidence alone
does not authorize removal: each named candidate also needs the required actual
runtime pull or verified reproducible rebuild recovery proof, current consumer
checks, and the project's normal non-force image removal gates. The settled Alloy
inventory is not reopened by this evidence request.
