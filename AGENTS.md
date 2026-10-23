# KuchDesk project rules

## Local service access

Follow [the canonical access guide](docs/local-service-access.md) for the
`kuchdesk-fresh` profile. Preserve each Service's Helm owner and internal DNS.
Allocate explicit TCP and UDP publication ports in `20000–20099` per protocol; Lima may also
discover unrelated VM listeners in that range, so verify owner and backend.
Never assume container localhost reaches the Mac; verify bridge routing. Do not
silently bind Mac ports to a wildcard, kill another process's listener, repeat
a completed Colima restart, or claim a path works from chart rendering or TCP
connectivity alone. Distinguish planned, implemented, installed and live
verified states. Offline checks do not prove scheduled Host Agent or MCP health.
Keep credentials out of commands, logs, and reports. Shared PostgreSQL, Redis and RabbitMQ definitions
retain Finance server behavior; preserve app ENV names and keep server ENV
separate. Fresh credentials are explicit operator decisions and Helm consumes
Secret references only. Preserve source definitions until a verified cutover.
Keep service YAML split by purpose, not duplicated: tracked `values.yaml` holds
non-secret chart defaults; machine-specific or credential files stay ignored
and mode 0600, following that service's README (`values.local.yaml` when the
chart consumes the private overlay, or `auth.local.yaml` when credentials are
provisioned separately as Kubernetes Secrets). Tracked examples contain only
placeholders. Keep private files out of chart packages with `.helmignore` where
they are in a chart directory. Do not assume every local YAML is excluded from
Helm input; verify the documented install path.
Measure ServiceLB source identity before changing ingress policy. Development
SMTP may serve Mac, ordinary-container and Pod clients; keep it separate from
its inbox API and HTTPS access. A cleared Mac port collision is not proof
that Lima recovered; verify listener ownership and backend identity without
killing the other process or restarting Colima.
TCP and UDP use separate allocations. Lima2.2.1 has a documented cross-protocol
number-deduplication defect; do not hide it by weakening catalog validation.
Native TCP recovery receipts must match publication UID and SSH PID/start time
before cleanup. SSH recovery does not cover UDP; verify
the installed forwarder before claiming support. Status CLI/API/MCP calls are
read-only. Optional Host Bridge recovery has its own explicit private-YAML
setting and persistent retry budget; never enable it as a side effect of a
status request or change the scheduled DNS Agent lifecycle.

Host Bridge TLS must use the existing trusted Colima development CA. Keep its
private signing key in the Kubernetes Secret and distribute only the public CA
certificate to clients. The owned host certificate refresher may read the
task-owned TLS Secret and reload only the owned Host Bridge after verifying a
certificate change.

For UI domains, follow the access guide's verified route configuration. Preserve
native logins and keep MailDev inbox/API access separate from SMTP. Verify the
client address at Traefik before applying a Mac allowlist; a masqueraded node
address does not establish Mac-only access. Obtain the requested approval before
activating additional UI access, credentials or NetworkPolicy allowances. The
current UI/API release is kube-system/kuchdesk-ui; preserve backend owners.
Its private auth YAML must remain excluded by .helmignore as well as .gitignore.
The approved Local traffic policy has a VM wildcard health-check listener30689;
do not call that a Mac listener or claim LAN isolation. DNS and wildcard
certificate coverage alone are not successful UI acceptance.

## Project-built image versions

Assign an explicit source-traceable version to every project-built service image, using that service's verified existing convention. The Host deployment workflow uses `git-<12-hex-source-commit>` from clean source and pins deployment by digest. Record the full source commit, declared application/release version and platform separately; chart package versions are not image build versions. Never use `latest` as the only identity or overwrite content behind an existing immutable tag. Verify local/registry tag absence or use a supported path to reuse its exact pinned artifact; unknown lookup failures do not prove absence. Do not create a release or invent another tag scheme just to rebuild. Follow the existing [build/deploy image guidance](host-agent/README.md#image-versions), including the operator collision gate that the current CLI does not automate. Retain publisher versions/digests for upstream images.

## Avoid duplicate image work

Before a build or pull, check the exact content and platform in each required image store. Reuse a build only when its verified source, dependency, Dockerfile/configuration, explicit build arguments, builder configuration and platform fingerprints match the existing artifact, and trusted build provenance identifies that exact store image ID. A copied fingerprint label is insufficient. A matching tag alone is insufficient. Changed inputs, a missing artifact, an unverified dependency or an explicit rebuild/refresh request require the appropriate fresh operation, subject to the immutable-tag collision rule above. Confirm absence with a successful store inventory; lookup errors do not prove absence. Never overwrite an existing immutable tag to satisfy a rebuild request.

Reuse a pulled image only by its exact immutable digest and platform with complete content in the required store. Host Docker and each KIND node's containerd/CRI inventories are separate requirements; presence in one does not satisfy another. Serialize concurrent requests for the same image/store and recheck after acquiring the lock. Preserve mutable-tag update checks and refresh behavior; shared immutable blobs may be reused by the store without suppressing registry update checks. Do not introduce a broad mutable-tag cache.

The Host deployment helper implements a local immutable-tag gate on all build paths, opt-in offline build reuse (`reuseBuild` plus `platform`) bound to a private receipt of the exact Docker store/image ID, and per-node KIND digest/platform/content checks. Its process-local locks coordinate calls in one Host process; independent CLI invocations still need operator serialization. The current agent Dockerfile's mutable base/frontend and cache mounts are ineligible for build reuse, so a confirmed absent target follows the fresh `--pull` build path. A `platform` enables KIND reuse independently of build opt-in. Explicit `rebuild`/`refreshImages` bypass reuse but do not authorize overwriting an immutable tag. These source helpers do not automate arbitrary Helm/upstream pulls, host Docker upstream pulls, registry tag collision checks, the migrated namespace's separate deployment script, or live acceptance. See [the supported profile fields and limits](host-agent/README.md#avoiding-duplicate-builds-and-pulls).

## Local image retention after deployment

After a successful build/deploy or upgrade and verified service health, retain only the latest successful image for each KuchDesk-managed service or subservice in each required local image store/platform. This applies to the Host and Kubernetes agents, observability components, and all actual project services. Preserve the previous image during rollout and health verification only as needed for rollback.

Before removing any older local image:

- Establish project ownership, its exact content digest, tags, platform, and image store. Multiple tags on one digest are one image; Docker, node containerd/CRI, and registry copies are separate inventories.
- Check every container, desired workload, shared reference and rollback path using that digest, including stopped containers and scaled-to-zero workloads. If it remains used or shared, report the reference and resolve it safely within the authorized scope. Do not force deletion.
- Verify recovery by pulling the pinned digest from a retained source, or by verifying a reproducible rebuild from preserved source, build inputs and required dependencies. A mutable tag, cached layer or untested rebuild instruction is insufficient. Preserve unique local-only content until recovery is verified. If the user instead chooses to discard exact unrecoverable old images, obtain action-time confirmation naming the objects, lost content and affected rollback paths; that explicit discard scope can replace the recovery requirement for those objects only. Do not delete before confirmation or manufacture a backup without authorization.
- Remove only specifically identified older, unused project images using the store's supported image operations. Never use a global prune or delete unrelated images, volumes, Secrets or unique local-only data. Shared physical blobs must remain available to retained image references.
- Record the kept digest, recovery evidence, removed image references per store, and any blocked shared/in-use reference in the deployment completion report.

Follow [deployment completion checks](docs/deployment-completion.md). This rule does not authorize an immediate blind purge; each future cleanup needs its own verified project scope. Image cleanup is separate from persistent-data deletion, telemetry retention and software upgrades.

## Task branch and worktree completion

After an authorized task merge, check branches and worktrees independently before reporting completion. Removing a worktree does not remove its local or remote task branch. This checklist does not grant access to another host or permission to delete additional resources.

1. Inventory. Verify this repository, the agreed integration branch (normally `main`), and its configured remotes. On every authorized execution host, inventory local branches with `git branch --list` and filesystem worktrees with `git worktree list --porcelain`. Inventory actual remote branches with `git ls-remote --heads <remote>` for each relevant remote; cached remote-tracking refs alone are not fresh remote evidence. Git remote refs do not inventory worktrees on remote execution hosts: inspect those hosts separately through authorized access. If a host or remote cannot be checked, retain uncertain items and report the missing inventory.
2. Verify integration. Commit only completed task changes, preserve unrelated work, and follow required reviews, checks, branch protections and current push/merge permissions. Verify the final agreed remote main contains the reviewed task result, including the exact tested source. For a normal merge, prove the task tip is an ancestor of that remote main. For squash or rebase integration, match the reviewed result to the integrated commit and check for later unique task commits; do not treat a closed PR or an old passing check as proof. Reconcile an advanced base through the established workflow without rewriting another user's work.
3. Check every candidate. Evaluate each task-owned local branch, remote branch and worktree separately. Require verified integration, no unique unpreserved commits or files, and no active session, process, service or other task depending on the candidate. Inspect staged, unstaged, untracked and ignored files plus nested repositories before removing a worktree. Preserve useful artifacts in an agreed location and verify their preservation before removal. Preserve main/default, protected, shared and unrelated branches, release tags, and any item whose ownership, integration or active use is uncertain. Retain a verified recoverable reference to the original task tip.
4. Remove eligible items. Recheck the exact candidate tips and worktree state immediately before removal; stop that candidate if they changed. Use normal non-forcing operations: `git worktree remove <path>`, `git branch -d <branch>`, and, when remote deletion is authorized, `git push <remote> --delete <branch>`. Remove only the inspected eligible task items on authorized hosts/remotes. Do not use force deletion, reset, clean or broad pruning to defeat a refusal. If normal deletion refuses, preserve the item and report why; continue with independent eligible items. A local branch deletion does not remove a remote branch.
5. Verify completion. Read back local branches, actual remote branches and filesystem worktrees on the same authorized hosts/remotes. Confirm the agreed main and integrated commit remain intact. Report the integration SHA, removed branches/worktrees, and every kept task item with its reason and next action. Do not infer that branch cleanup succeeded from worktree removal alone, or claim a remote deletion from a cached ref disappearing.

Protected MailDev API uses the existing panel login for all client modes. Keep raw unauthenticated backend1080 denied outside Traefik; use the stable authenticated API Service instead. Finance host-test auth/port alignment remains separate; see [access guide](docs/local-service-access.md).

Shared MailDev API uses a stable authenticated Traefik Service for all Pods/ordinary containers/Mac clients. Do not pin access to ephemeral Pod IPs or enable raw unauthenticated1080. Preserve local source ranges and NodePort removal for MailDev publications; HTTPS panels retain their own policy.

Observability paths have separate evidence requirements: Pod stdout is collected through Kubernetes; Mac/container logs need explicit OTLP log exporters. OTLP metrics, traces and logs are distinct signals and each must be wired and queried before being called supported. Do not grant telemetry agents containerd-socket or broad host-file access for discovery. Inspect the Mac scrape bind address; a working scrape from a wildcard listener does not mean LAN exposure is safe. Prometheus currently has a fresh 10Gi local-path PVC; its prior emptyDir history was not migrated, and persistence across a later Pod replacement is not verified until a sample survives one.
