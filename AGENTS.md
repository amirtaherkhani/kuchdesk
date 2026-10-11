# KuchDesk project rules

## Project-built image versions

Assign an explicit source-traceable version to every project-built service image, using that service's verified existing convention. The Host deployment workflow uses `git-<12-hex-source-commit>` from clean source and pins deployment by digest. Record the full source commit, declared application/release version and platform separately; chart package versions are not image build versions. Never use `latest` as the only identity or overwrite content behind an existing immutable tag. Verify local/registry tag absence or use a supported path to reuse its exact pinned artifact; unknown lookup failures do not prove absence. Do not create a release or invent another tag scheme just to rebuild. Follow the existing [build/deploy image guidance](host-agent/README.md#image-versions), including the operator collision gate that the current CLI does not automate. Retain publisher versions/digests for upstream images.

## Local image retention after deployment

After a successful build/deploy or upgrade and verified service health, retain only the latest successful image for each KuchDesk-managed service or subservice in each required local image store/platform. This applies to the Host and Kubernetes agents, observability components, and all actual project services. Preserve the previous image during rollout and health verification only as needed for rollback.

Before removing any older local image:

- Establish project ownership, its exact content digest, tags, platform, and image store. Multiple tags on one digest are one image; Docker, node containerd/CRI, and registry copies are separate inventories.
- Check every container, desired workload, shared reference and rollback path using that digest, including stopped containers and scaled-to-zero workloads. If it remains used or shared, report the reference and resolve it safely within the authorized scope. Do not force deletion.
- Verify recovery by pulling the pinned digest from a retained source, or by verifying a reproducible rebuild from preserved source, build inputs and required dependencies. A mutable tag, cached layer or untested rebuild instruction is insufficient. Preserve unique local-only content until recovery is verified. If the user instead chooses to discard exact unrecoverable old images, obtain action-time confirmation naming the objects, lost content and affected rollback paths; that explicit discard scope can replace the recovery requirement for those objects only. Do not delete before confirmation or manufacture a backup without authorization.
- Remove only specifically identified older, unused project images using the store's supported image operations. Never use a global prune or delete unrelated images, volumes, Secrets or unique local-only data. Shared physical blobs must remain available to retained image references.
- Record the kept digest, recovery evidence, removed image references per store, and any blocked shared/in-use reference in the deployment completion report.

Follow [deployment completion checks](docs/deployment-completion.md). This rule does not authorize an immediate blind purge; each future cleanup needs its own verified project scope. Image cleanup is separate from persistent-data deletion, telemetry retention and software upgrades.
