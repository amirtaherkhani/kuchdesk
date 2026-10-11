# KuchDesk project rules

## Local image retention after deployment

After a successful build/deploy or upgrade and verified service health, retain only the latest successful image for each KuchDesk-managed service or subservice in each required local image store/platform. This applies to the Host and Kubernetes agents, observability components, and all actual project services. Preserve the previous image during rollout and health verification only as needed for rollback.

Before removing any older local image:

- Establish project ownership, its exact content digest, tags, platform, and image store. Multiple tags on one digest are one image; Docker, node containerd/CRI, and registry copies are separate inventories.
- Check every container, desired workload, shared reference and rollback path using that digest, including stopped containers and scaled-to-zero workloads. If it remains used or shared, report the reference and resolve it safely within the authorized scope. Do not force deletion.
- Verify recovery by pulling the pinned digest from a retained source, or by verifying a reproducible rebuild from preserved source, build inputs and required dependencies. A mutable tag, cached layer or untested rebuild instruction is insufficient. Preserve unique local-only content until recovery is verified.
- Remove only specifically identified older, unused project images using the store's supported image operations. Never use a global prune or delete unrelated images, volumes, Secrets or unique local-only data. Shared physical blobs must remain available to retained image references.
- Record the kept digest, recovery evidence, removed image references per store, and any blocked shared/in-use reference in the deployment completion report.

Follow [deployment completion checks](docs/deployment-completion.md). This rule does not authorize an immediate blind purge; each future cleanup needs its own verified project scope. Image cleanup is separate from persistent-data deletion, telemetry retention and software upgrades.
