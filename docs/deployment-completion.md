# Deployment completion and local images

For each authorized build/deploy or upgrade, verify the running service and its integration before applying the project image rule in [AGENTS.md](../AGENTS.md). The target is the latest successful image per KuchDesk-managed service/subservice, with the required platform and runtime copies accounted for.

1. Assign and record the explicit source-traceable build version before deployment, following [image version guidance](../host-agent/README.md#image-versions). Record the declared service version separately from its build tag and chart version; do not overwrite an existing immutable tag. Record the before/after service image, immutable digest, platform and deployment/release identity. Keep the prior image during rollout and health verification for the agreed rollback.
2. Verify readiness, the service's authenticated API or ingestion/query path, relevant metrics and alerts, and its actual dependencies. Report a failed integration as unresolved; a build or Ready Pod alone is insufficient. Do not start cleanup while the success gate remains unresolved.
3. Inventory each relevant image store separately: host Docker, each Kubernetes node's containerd/CRI, and any project registry. Group tags by content digest. A manifest index, platform manifest and runtime copy are distinct references; do not assume removing one reference removes a physical image or its other copies.
4. Identify older project-owned images and all references: running/stopped containers, desired workloads including zero replicas, other services, shared digests, and recovery sources. Verify a pinned recovery pull or reproducible rebuild before removal. Resolve still-used references only within the current authorization; never force deletion or erase unique local-only content.
5. Remove the exact unused candidates with supported store operations. Record the latest retained digest per service/store/platform, tested recovery source, removed references and remaining shared/in-use blockers. Global image/system prune is excluded; volumes, Secrets, unrelated images and persistent data are excluded.

An upstream tag alone does not prove the deployed digest is recoverable. A pull check should fetch the pinned digest/platform from the retained source; a rebuild check needs preserved source, Dockerfile/build context, pinned dependencies and outputs that satisfy the deployment's recovery requirement. Do not fetch excluded SDK upgrades or silently replace build inputs to make that check pass. If recovery is unverified, preserve the image and report the blocker.

This is a completion rule, not an image garbage-collection daemon. No images were removed when the rule was saved. Persistent history/volume deletion requires its separate approved scope.

## Migrated agent release

The KuchDesk agent's workload namespace is `platform-tools`; its existing Helm release record remains `development-tools`. Follow the [scoped agent renderer workflow](../lab/migrations/agent-platform-tools/README.md). The generic Host deployment workflow currently lacks post-renderer support and checks readiness in its release namespace, so it is not a supported upgrade route for this migrated release. Do not use an ordinary unrendered Helm upgrade on it.
