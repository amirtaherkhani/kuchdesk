# KuchDesk Host DNS Agent

The Go CLI discovers the default-route interface and its private IPv4 address, opens a localhost-only Kubernetes port-forward to Technitium's admin API, and reconciles one configured DNS wildcard. It reads the admin password from a Kubernetes Secret; no credential or host IP is stored in this repository. It skips writes when the DNS record is already current. The supported host installation is macOS: the installer configures a per-user `launchd` job. Existing non-macOS source is retained but is outside this version's supported host workflow.

Prerequisites: a reachable Kubernetes cluster with a healthy Technitium Service, `kubectl`, Go for building, and access to the Technitium Secret. The macOS installer pins the selected Kubernetes context in the LaunchAgent, so later changes to the shell's current context do not redirect DNS writes. `RunAtLoad` and a 30-second interval cover IP changes and restart recovery on macOS. Install for the current macOS user:

```sh
KUCHDESK_HOST_AGENT_KUBE_CONTEXT=docker-desktop \
KUCHDESK_HOST_AGENT_ZONE=local.dev \
./host-agent/install-macos.sh
launchctl print "gui/$(id -u)/dev.kuchdesk.host-agent"
```

The installer requires `KUCHDESK_HOST_AGENT_ZONE` and otherwise uses the current `kubectl` context unless `KUCHDESK_HOST_AGENT_KUBE_CONTEXT` is set. To use another Technitium deployment, pass `KUCHDESK_HOST_AGENT_NAMESPACE`, `KUCHDESK_HOST_AGENT_SERVICE`, `KUCHDESK_HOST_AGENT_ADMIN_SECRET`, `KUCHDESK_HOST_AGENT_PASSWORD_KEY`, `KUCHDESK_HOST_AGENT_ADMIN_USER`, or `KUCHDESK_HOST_AGENT_API_PORT` to the installer. `KUCHDESK_HOST_AGENT_INTERFACE` selects a non-default interface; `KUCHDESK_HOST_AGENT_TARGET_IP` sets an explicit IPv4 address directly, including loopback for host-only DNS. `KUCHDESK_HOST_AGENT_TTL` controls the wildcard record TTL (default 30 seconds), `KUCHDESK_HOST_AGENT_TIMEOUT` limits a single run (default 30 seconds), and `KUCHDESK_HOST_AGENT_INTERVAL` controls the launchd interval (default 30 seconds; allowed 5–3600). The installer saves the discovered absolute `kubectl` path as an argument, so launchd does not depend on a shell PATH. A non-default `KUBECONFIG` is retained as absolute paths by the LaunchAgent.

For a single reconciliation without installing launchd, pass the zone and context explicitly:

```sh
cd host-agent && go run ./cmd/kuchdesk-host-agent -kube-context docker-desktop -zone local.dev
```

For a Mac whose default route does not lead to the intended ingress, pass `-interface en0` or an explicit `-target-ip` after checking the actual local network. The CLI also accepts `-kubectl`, `-ttl`, and `-timeout` for direct runs.

If a VPN changes the default route to a tunnel interface, the agent refuses to publish that address. On a multi-interface host, use `-interface` with the interface that reaches the local ingress after checking the routing profile, or use `-target-ip` with the intended IPv4 address. Automatic selection still chooses a private address; an explicit loopback address is useful only when the DNS clients run on that same host. The agent currently handles IPv4 only. It updates only the configured zone's wildcard DNS **answer**; it does not set system-wide DNS servers or routing.

The agent's Kubernetes credential can read the existing admin Secret. Treat that user account and kubeconfig as privileged; a dedicated scoped Technitium API credential and finer host permissions belong to later hardening work. The admin API is reachable only through a localhost port-forward.

The DNS reconciler opens its own temporary localhost port-forward. This repository does not install a fixed-port Technitium admin forwarder or a local image registry.

## Doctor diagnostics

`cmd/kuchdesk-doctor` runs bounded, read-only checks of the Mac default route,
one configured DNS name, Docker, Kubernetes node readiness, a loopback
registry, build-volume free space, node metrics, and optional Deployment
readiness. It reports ordered checks, sanitized evidence, findings, and
specific recommendations as JSON. A VPN-owned default route is a warning.
The command does not change network settings, cluster resources, or Secrets.

```sh
cd host-agent
go run ./cmd/kuchdesk-doctor \
  -domain infisical.local.dev \
  -kube-context docker-desktop \
  -registry-url http://127.0.0.1:5001 \
  -disk-path /absolute/path/to/project \
  -service platform-tools/kuchdesk-agent \
  -service platform-system/kuchdesk-local-registry
```

The `kuchdesk_doctor` MCP tool uses the same collector and returns report schema
`kuchdesk.doctor/v2`: ordered check/component/source/context/timing/status,
typed findings, dependencies, limitations, and short redacted command excerpts.
For AI-assisted diagnosis, use the existing MCP client's
`kuchdesk_doctor_repair` prompt. It creates a fresh report and supplies the
catalog of implemented typed tools. The report schema and English prompt
template are available as MCP resources at `kuchdesk://doctor/report/v2` and
`kuchdesk://doctor/prompt/v2`, and as
[`internal/doctor/doctor-report.schema.json`](internal/doctor/doctor-report.schema.json)
and [`internal/doctor/prompt.md`](internal/doctor/prompt.md) in source.
No separate model, provider, API key, or CLI `-ai` mode is used.

The prompt requires a bounded typed plan through `kuchdesk_doctor_validate_plan`,
uses `kuchdesk_deploy_plan` and `kuchdesk_deploy_preflight` before an authorized
`kuchdesk_deploy_start`, waits for job status, and reruns affected checks with
`kuchdesk_doctor_verify`. Only the deployment start is a supported repair
action in the current MCP catalog. It cannot repair DNS, firewall, RBAC, or
credentials by inventing commands. Plan validation is not authorization;
the existing deployment opt-in and exact confirmation still apply. Failed
post-checks remain unresolved.

## Infisical MCP: first implementation slice

`cmd/kuchdesk-infisical-mcp` is a separate, long-running stdio MCP server. It does not change the periodic DNS LaunchAgent or run as a network listener. It uses the official MCP Go SDK v1.6.0 and Infisical's documented REST API from the deployed v0.151.0 OpenAPI; the host-agent Go module now requires Go 1.25. The local client implements Universal Auth login, in-memory token renewal from `expiresIn`, restart re-login, one bounded retry of a read after HTTP 401, and a pause after a repeated 401. It does not assume a year-long access token.

The Infisical CLI and MCP `all` scope use one explicitly selected credential backend. The
default `env` backend reads only the KuchDesk project's `.env` file. Find the
project from the working directory or set `KUCHDESK_PROJECT_ROOT` to its
absolute path. A missing `.env` leaves the client unconfigured; an existing
file is never created or overwritten by KuchDesk. Keep it user-owned, mode
`0600`, and Git-ignored. [`.env.example`](../.env.example) contains placeholders
only. The parser reads `INFISICAL_CLIENT_ID` and `INFISICAL_CLIENT_SECRET` as
literal values without shell evaluation and requires both together. Other
project settings can remain in the same file.

On macOS, set `KUCHDESK_INFISICAL_CREDENTIAL_BACKEND=keychain` and
`KUCHDESK_INFISICAL_KEYCHAIN_ACCOUNT` to select one existing generic-password
item in service `kuchdesk.infisical.machine-identity`; the account is the client
ID and the item password is the client secret. Keychain is never queried in
`env` mode. No backend fallback or automatic migration occurs. This change
does not create a Keychain item or a credential file. Do not put values in
command arguments, Git, MCP configuration text, prompts, or chat. Pass the
HTTPS origin separately:

```sh
cd host-agent
go run ./cmd/kuchdesk-infisical-mcp -url https://YOUR-INFISICAL-HOST
```

The default `-scope all` exposes the Infisical name-listing tools, Doctor,
and the opt-in deployment workflow. `-scope doctor-repair` exposes only Doctor
and deployment tools, including the typed repair-plan validator and prompt;
it needs no Infisical URL or credential. Deployment remains disabled unless
`KUCHDESK_DEPLOY_ENABLED=true` is set with a trusted profile directory and an
exact release/namespace confirmation. `-scope doctor-readonly` exposes only
Doctor and post-check verification with the report schema; it does not offer
repair tools or the repair prompt. For example, a local diagnosis can start
with `go run ./cmd/kuchdesk-infisical-mcp -scope doctor-readonly`. These modes
select tools in the server itself; no MCP client registration has been made.

Restart the MCP stdio process after changing backend configuration or rotating
an existing credential. Test locally with `kuchdesk-infisical ... capabilities`
(`configured:true` proves only that the selected pair loaded). Use
`check-project-access -slug PROJECT_SLUG` to inspect metadata visibility before
trying `list-secret-names` with the intended project ID, environment, and narrow
path. An empty project list is scoped to the current machine identity: it does
not prove that no projects exist. The detail result distinguishes `forbidden`
from `not_found` without printing an API response body. The existing Grafana
Operator Secret uses a separate identity and must not be copied into the host
backend.

The Infisical MCP tools are `infisical_capabilities`, `infisical_list_secret_names`, `infisical_start_list_secret_names`, `infisical_job_status`, and `infisical_cancel_job`. Name listing requires an explicit project ID, environment slug, and absolute secret path. It requests `viewSecretValue=false` and returns only names and paths, discarding any value fields from the API response. Discovery works without credentials; a secret-list call then returns `credentials_not_configured`.

The persistent MCP process executes up to four independent operations in parallel, retains at most 64 in-memory records, and gives each operation a 30-second deadline. Operations targeting the same nonempty resource key serialize in the runner; the current name-only reads use no key. Status distinguishes queued, running, succeeded, failed, canceled, and timed out. A cancellation request is cooperative through Go context; process exit discards job history and cancels in-flight work. This is lightweight agent execution, not a durable task service.

The command-line client shares the same typed Infisical service and runs synchronously:

```sh
cd host-agent
go run ./cmd/kuchdesk-infisical -url https://YOUR-INFISICAL-HOST capabilities
go run ./cmd/kuchdesk-infisical -url https://YOUR-INFISICAL-HOST check-project-access -slug PROJECT_SLUG
go run ./cmd/kuchdesk-infisical -url https://YOUR-INFISICAL-HOST list-secret-names -project PROJECT_ID -environment dev -path /app
```

The CLI check does not create identities, change roles or project memberships,
or provision a live MCP service. It verifies metadata visibility only; secret
access, bootstrap credential lifetime, and other permissions require separate
checks.

The opt-in [host control plane](../docs/host-control-plane.md) now has typed
project, environment, folder, secret, role, and machine-identity membership
commands. `kuchdesk-host-bridge` is a separate HTTPS listener and is **off**
unless `KUCHDESK_HOST_BRIDGE_ENABLED=true`. It requires `-url`, `-tls-cert`,
`-tls-key`, and `-project-ids`; `-allow-project-create` is another explicit
gate. `KUCHDESK_HOST_BRIDGE_TOKEN` is loaded from the same user-owned,
mode-0600 project `.env` as the Infisical pair. Start it from the checkout or
set `KUCHDESK_PROJECT_ROOT` to an absolute checkout path. An approved operator
must choose the address, TLS identity, project allowlist, and token before
running it. A loopback listener is the default and cannot be reached from the
cluster. This Mac's live listener uses a dedicated bearer and certificate and
allows only Kubedesk Platform (`home-lab-nb0-k`); see the
[project onboarding boundary](../docs/infisical-project-onboarding.md) before
adding another project.

For an explicitly approved AI management client, the existing stdio MCP server
can add `infisical_manage` with `-scope all -manage -project-ids PROJECT_ID`.
This uses the same typed host command service, returns no secret values, and
is not part of `doctor-repair`. The live MCP registration is `doctor-repair`
only. Existing Grafana Operator sync remains independent.

## Local build and deployment workflow

`cmd/kuchdesk-deploy` is a local operator CLI. Its default mode prints a plan;
`-apply` runs it only with an exact `release/namespace` confirmation. A profile
is one JSON object with absolute `sourceDir`, `dockerfile`, and `chartDir`
paths, optional absolute `valuesFiles`, `imageRepository`, a 12-hex-commit
`imageTag` such as `git-123456789abc`, `kubeContext`, `namespace`, `release`,
`deployment`, and `test` (`go` or empty). Dockerfile and chart must be inside
the clean Git source tree. The image build tag must match that source's HEAD. The CLI
also requires `preflightDomain` (a host-resolvable DNS name) and
`preflightRegistryUrl` (a loopback registry origin). For the `host-crane`
profile, the registry URL must point to the local port 5001.
uses first-party Docker, Helm, kubectl, and Go commands with explicit arguments
and context; it never invokes a shell or passes Infisical bootstrap variables
to child commands. Values files are operator-supplied; review them before
apply and reference Kubernetes Secrets by name instead of putting secret
values in Helm release metadata.

For the KuchDesk agent chart, a values file must also provide `cluster.id`
and `cluster.name`; the workflow supplies image repository, tag, and digest.

### Image versions

Every project-built service image needs an explicit, traceable build version.
Reuse this workflow's existing `git-<12-hex-source-commit>` convention; it rejects
`latest` and requires the tag to match the clean build source's HEAD. Record the
full source commit and the service's declared application/release version as
separate fields. The current agent declares app version `0.10.0`; chart package
version `0.13.0` is not its image version. This guidance does not create a release
or change either version.

From the profile's actual source directory, derive the existing image tag:

```sh
SOURCE_COMMIT="$(git -C "$SOURCE_DIR" rev-parse HEAD)"
IMAGE_TAG="git-$(git -C "$SOURCE_DIR" rev-parse --short=12 HEAD)"
```

Before building or pushing, inspect the exact local and destination-registry
tag. An existing immutable tag must keep its original content: reuse its
verified digest through a supported deployment route, or stop. A failed lookup
is not evidence of absence; distinguish confirmed absence from connection,
authentication and authorization failures. Never overwrite the tag with a
different image, including a rebuild with changed base images or build inputs.
Commit those changed inputs before deriving another build version. Do not add a
timestamp, SemVer suffix or new release tag that this CLI does not support.

The CLI checks source/tag agreement, local immutable-tag collisions and digest
pinning. It can reuse an eligible exact local build as described below; push
and registry digest resolution still run. Registry tag collisions remain a
required operator gate before `-apply`: inspect the destination and verify
that any already published tag is the exact intended artifact. Registry
immutability enforcement must be verified separately. For upstream service images, preserve their
publisher's explicit version and digest rather than relabelling them as a local
build.

### Avoiding duplicate builds and pulls

Profiles may set `platform` to one explicit Linux `os/architecture[/variant]`,
`buildArgs` to a map of explicit build argument values, `reuseBuild` to opt into
offline build reuse, `rebuild` to force a build without cached layers, or
`refreshImages` to bypass both build and KIND pull reuse. These controls retain
the existing exact deployment confirmation and clean-source/tag requirements.
Do not place credentials in build arguments or image labels.

Every build first inspects its exact local tag. If inspection fails, a
successful exact-tag Docker inventory must confirm absence; unavailable or
contradictory evidence blocks the build. A known existing immutable tag is
reused only with an eligible matching fingerprint, actual platform/store image
ID and a matching trusted build receipt. Otherwise apply stops with a collision
error, including explicit rebuild
or refresh of an already tagged image. For changed inputs, commit those inputs
and derive the resulting `git-<12-hex-source-commit>` tag; rebuild only a
confirmed absent target. This local gate does not prove registry absence.

Eligible opt-in builds hash the full local context's names, modes and bytes
(including dependency/lock/config files and ignored files), Dockerfile path,
explicit arguments, platform, offline network configuration, explicit Docker
context builder and Docker/BuildKit settings, Docker server configuration and
verified base configurations. The
fingerprint is recorded on the built image as
`io.kuchdesk.build-fingerprint.v1`. After a successful build, the helper verifies
the actual image ID, platform and label, then atomically records a small receipt
in the current user's OS cache under `kuchdesk/build-receipts` (0700 directory,
0600 files). The receipt binds the proven Docker context and daemon ID, image
reference, platform and fingerprint to the exact store image ID. Reuse must
match that ID independently of the image's copied or inherited labels. Missing,
corrupt, overly permissive or symlink receipt metadata blocks reuse of an
existing tag; old images are never adopted from their labels alone. Receipt
write or post-build identity verification failures stop apply before push.
These receipts contain metadata only, and are kept outside the build context.
They rely on the current user's private cache ownership; they are not signed
provenance or image backups. Docker store image IDs may identify root/index or
config objects; they are not treated as registry digests or KIND CRI IDs.

Bases must be `scratch`, verified pinned local digest/platform artifacts
without inherited `ONBUILD`, or earlier stages. Reuse disables for mutable or
unavailable bases, custom frontends selected by
case/whitespace-normalized `syntax`/`escape` directives or an explicit
`BUILDKIT_SYNTAX` argument, remote `ADD`/external `COPY`, build mounts, line continuations, symlinks,
special files, an implicit platform or a `DOCKER_HOST` endpoint override. This
deliberately narrow Dockerfile subset avoids guessing at external dependencies.
Eligible builds require BuildKit and run with
`--builder <Docker-context> --network none`; dependencies must already be in
the context/base, such as vendored Go modules. Eligible offline builds explicitly provide every
predefined proxy argument (`HTTP_PROXY`, `HTTPS_PROXY`, `FTP_PROXY`, `NO_PROXY`,
`ALL_PROXY` and their lowercase variants), defaulting unspecified values to
empty while preserving operator-supplied values. These effective arguments are
fingerprinted, so Docker's implicit client-config proxies cannot alter the
build. The helper does not read credential-bearing Docker client config;
default fresh builds retain normal client-config behavior. See Docker's
[frontend selectors](https://docs.docker.com/build/buildkit/frontend/) and
[proxy configuration](https://docs.docker.com/engine/cli/proxy/) documentation.
A failed offline build remains a failure and is never silently retried with
network access.

The current agent Dockerfile uses a mutable Go base, a mutable syntax frontend
and cache mounts, so it remains ineligible. For a confirmed absent tag, fresh
builds use `--pull`, which checks base updates while Docker may reuse existing
immutable blobs/layers. Rebuild/refresh also adds `--no-cache`. This does not
promise zero registry traffic, automate dependency updates, or relax tag
immutability.

With `kindPrepull` and an explicit `platform`, each discovered node is checked
independently: `crictl inspecti` must show the exact repo digest and actual
OS/architecture/variant, and `ctr -n k8s.io images check --quiet` must show
complete content for the requested reference. Host Docker presence cannot
satisfy either node inventory. Missing/mismatching/unknown node evidence runs
the existing `ctr` plus CRI pull steps, with an explicit containerd platform,
and the helper verifies the digest/platform/content again before Helm. The
normal transfer pull retains its configured verifier policy. If the exact
pinned reference is still incomplete afterward, a guarded `ctr content fetch`
restores its compressed content before CRI registration. This handles existing
unpacked snapshots whose compressed blobs are absent; the shared node/image
lock covers the repair as well. No global containerd or security setting changes.
With an
implicit platform, or a mutable reference, it retains the fresh pull path.
`refreshImages` bypasses these reuse checks. KIND reuse is independent of
`reuseBuild`; this helper creates no host Docker upstream-pull cache.

Process-local locks serialize the image check/build and each node's
check/pull, then inspect the store again for waiting callers. Independent CLI
processes require operator serialization. Helm lint/tests, Doctor checks,
push/digest resolution, Helm upgrade and rollout checks still run; the current
namespace limitation below still applies. Fake-store tests cover reuse,
changed inputs, immutable collisions, absent/unknown images, platform/variant
mismatches, per-node content, concurrent calls, explicit bypass, preserved-label
image replacement, daemon identity, missing/corrupt/private receipt metadata,
frontend selector variants and implicit/explicit proxy inputs. These tests
are source verification. Separately authorized [bounded live acceptance](../docs/observability-source-acceptance-2026-10-11.md#bounded-live-image-reuse-acceptance)
uses real helpers and ExecRunner with tiny scratch inputs, retained receipts,
and the exact existing agent digest. It applies no Helm release, starts no
container, changes no access, and removes no image. The opt-in tests
`TestLiveImageReuseAcceptance` and `TestLiveReuseResumeAcceptance` skip by
default. They require `KUCHDESK_LIVE_IMAGE_ACCEPTANCE=confirm-scratch-builds-and-current-agent-pull`,
an absolute `KUCHDESK_LIVE_IMAGE_ARTIFACT_DIR`, the explicitly approved
`KUCHDESK_LIVE_KIND_NODE` and pinned `KUCHDESK_LIVE_KIND_IMAGE`; resume also
requires the retained `KUCHDESK_LIVE_FIXTURE_SOURCE`. Run only the selected
live test under `go test -race -count=1 ./internal/deploy -run '^TestLiveImageReuseAcceptance$' -v`
after confirming the current node/image scope. The initial test allows at most
two builds; resume refuses every build. Both limit each required node operation
to one per run. All fixtures and images remain for evidence/recovery. This
does not replace the migrated agent's deployment renderer or authorize runtime
service installation/restarts.

Record the image repository/tag, source commit, declared service version,
platform, build inputs and resolved registry/deployed digest. Keep host Docker
index IDs, platform manifests and node CRI config IDs distinct. After verified
health, follow the [completion rule](../docs/deployment-completion.md): retain
the latest successful local version per service/store/platform and remove only
unused predecessors with verified pinned recovery. Tags sharing a digest are
one object; protect active/shared/system and unique unrecoverable content.

The migrated home-lab agent runs in `platform-tools`, while Helm metadata stays
in `development-tools`. This generic workflow lacks the required post-renderer
and targets rollout checks at the release namespace; use the [scoped migration
workflow](../lab/migrations/agent-platform-tools/README.md) for that release.
The example below is for a profile whose workloads share its release namespace.

```sh
cd host-agent
go run ./cmd/kuchdesk-deploy -profile /absolute/path/to/profile.json
go run ./cmd/kuchdesk-deploy -profile /absolute/path/to/profile.json -preflight
go run ./cmd/kuchdesk-deploy -profile /absolute/path/to/profile.json \
  -apply -confirm kuchdesk-agent/development-tools
```

Apply first runs the read-only Doctor checks for the default route, DNS,
Docker, Kubernetes node readiness, registry, source-volume free space, and
node CPU/memory use. Any failed, warning, or unavailable check blocks apply
before source validation and build. It then runs Helm lint, optional
`go test ./...`, Docker build/push, image digest
resolution, Helm template, atomic Helm upgrade, and `kubectl rollout status` in
that order. It stops before Helm if push or digest lookup fails. The Helm chart
uses `image.digest` to pin the pulled content. The CLI prints step names as
progress and reports failures without printing child output, rendered Secret
data, or Infisical credentials. A successful rollout is readiness evidence,
not an application smoke test. Helm's `--atomic` covers upgrade failures; if the subsequent
`kubectl rollout status` check fails, inspect the release before retrying
because the successful Helm upgrade remains installed. The workflow has
fake-runner and chart-render tests. A host-side `crane` push and node-side
`ctr` pull have succeeded against the live registry. A temporary unprivileged
Pod also reached `Pulled`, `Created`, and `Started` with
`imagePullPolicy=Always`; it exited because the deliberately omitted
`KUCHDESK_CLUSTER_ID` is required and was then removed. The image layers were
already cached. A manual Helm install and a subsequent full CLI build/push/
upgrade reached 1/1 Ready; the latter needed a corrective CRI pull after the
older `ctr` pre-pull step. A fresh-blob kubelet download remains unverified.

When a registry is available only through a macOS localhost port-forward,
Docker Desktop's image daemon cannot use that host-local endpoint. Set
`pushMode` to `host-crane`, `hostPushRepository` to
`127.0.0.1:5001/kuchdesk-agent`, and `imageRepository` to
`localhost:5001/kuchdesk-agent`. Install `crane` v0.20.6 from
`github.com/google/go-containerregistry/cmd/crane` on the host PATH. The
workflow saves the built Docker image to a temporary archive, pushes it from
the host with `crane`, and resolves its registry digest. `kindPrepull: true`
discovers the nodes from the explicit Kubernetes context, fetches the exact
tag and digest through `ctr`, then registers that reference through `crictl`
(the kubelet's CRI). Helm uses `image.pullPolicy=Never` only after both steps
succeed on every node. A direct CRI digest fetch short-read this registry's
manifest in the current Docker Desktop KIND runtime; `ctr` fetched the same
digest successfully, and CRI then recognized it. `ctr` alone is insufficient
for kubelet visibility. This is a Docker Desktop KIND profile, not a generic
Kubernetes registry solution.
The corrected full CLI apply completed on this installation: the pushed image
was digest-pinned, Helm revision 4 deployed, and the new agent Pod reached
1/1 Ready. An authenticated capabilities request for `development-tools`
returned 218 resources in 0.030 seconds; the earlier build took 39.78 seconds.
The registry Service and persistent data are retained. Existing cloud-hosted
images in other charts are unaffected.

The host MCP process offers `kuchdesk_deploy_plan`, `kuchdesk_deploy_preflight`, `kuchdesk_deploy_start`,
`kuchdesk_job_status`, and `kuchdesk_cancel_job`. Set an absolute
`KUCHDESK_DEPLOY_PROFILE_DIR` to permit plan reads of named `.json` files in
that directory. Start stays disabled unless `KUCHDESK_DEPLOY_ENABLED=true`
is set in the trusted host process environment; it also requires the exact
`release/namespace` value from the profile. A start runs in the existing
bounded in-process runner with a 20-minute deadline and serializes operations
for the same context, namespace, and release. Profile names cannot escape the
configured directory through path traversal or symlinks. Status and cancel
are per-process only. The MCP process requires local Docker, `crane`, Helm,
kubectl, and access to the selected Kubernetes context. This deployment path
has source tests but has not performed a live MCP-driven rollout. The separate
host CLI and manual Helm rollouts have succeeded. CoreDNS edits remain a
separate operator decision.

## Hybrid enrollment and base project metadata

The optional local [hybrid controller](../docs/infisical-hybrid-control.md) separates
human project enrollment from routine machine authentication. Its policy defaults
to dry-run. No human session or new access is installed by building the binary.
Use the [user-run browser login and status commands](../docs/infisical-human-login.md)
for supported v0.151.0 authentication without copying tokens.
The network bridge permits only read operations; use the separately scoped local
CLI/MCP for administrative operations.

Set `KUCHDESK_BASE_PROJECT_NAME` in the private project `.env` to change the base tools project's display
label (default `Kubedesk Platform`). Optional `KUCHDESK_BASE_PROJECT_ID` and
`KUCHDESK_BASE_PROJECT_SLUG` are explicit metadata selectors, never inferred from
the name. Managed projects always retain independent explicit IDs and policies.
Environment changes require restart; enrollment policy files reload at runtime.

The Host metadata loader uses non-empty process environment, then the private
project `.env`, then defaults. It reads only the three `KUCHDESK_BASE_PROJECT_*`
keys and never exports or evaluates file contents. Keep the same private-file
permissions and literal `KEY=value` syntax used for credentials. File metadata is re-read on each CLI invocation and MCP capabilities request.
Process environment changes require a restart; enrollment include/exclude
policy continues to reload independently. Cluster processes use Helm values,
not the Host credential file.

### Explicit Host Agent Admin login

`kuchdesk-host-agent admin-login --project-root /absolute/project` opens the
configured official Infisical browser login and provides hidden paste recovery.
You can instead set `KUCHDESK_PROJECT_ROOT` to an absolute root and run the
installed binary from any directory. Use `admin-login --help` for usage.
This is an explicit interactive command: ordinary DNS reconciliation and daemon
startup never prompt. It reuses the enrollment CLI implementation, requires an
interactive terminal and existing private enrollment policy/state, and refuses
a competing controller writer. Stop the controller before login, then resume it
after successful validation; this command does not manage service lifecycle.
See [human login](../docs/infisical-human-login.md) for browser recovery and status.

### Manual Grafana password recovery

Build the existing Go Host Agent from the verified source checkout:

```sh
cd host-agent
go build -o /tmp/kuchdesk-host-agent ./cmd/kuchdesk-host-agent
```

Run these commands yourself in your own interactive Terminal. Retrieval reads
only `observability/grafana-admin` key `GRAFANA_ADMIN_PASSWORD` using the explicitly
selected Kubernetes context and existing `get Secret` access:

```sh
/tmp/kuchdesk-host-agent grafana-admin-password --kube-context docker-desktop
```

Type `REVEAL GRAFANA BOOTSTRAP PASSWORD` at the prompt to display that value.
The value remains in the terminal's scrollback. Cancellation does not fetch it.
This is the Infisical-managed **bootstrap copy**, not a lookup of the current
Grafana database password. Grafana uses its initial admin password on database
initialization; a later database password reset can leave this copy stale. If
the database password differs, the retrieved bootstrap value will not sign you
in. The command cannot determine or reveal the database password.

To set a new database password, verify the admin account's numeric ID through
your existing admin session, then run:

```sh
/tmp/kuchdesk-host-agent grafana-admin-password-reset \
  --kube-context docker-desktop --user-id VERIFIED_ADMIN_ID
```

Enter the new password twice with input hidden, then type
`RESET GRAFANA ADMIN PASSWORD` to submit it. The CLI default ID is `1`; that
default does not verify the account. Passwords must be printable UTF-8, at most
4096 bytes, without surrounding whitespace. Ctrl-C/Ctrl-D cancel hidden input
and restore terminal echo. The underlying Grafana CLI may enforce additional
password requirements. Failures stop without retrying a reset automatically.

Reset uses existing Kubernetes access to the fixed Grafana Deployment/container
and the installed Grafana CLI with its current configuration and
`/var/lib/grafana` data path. The password is sent only on stdin. Infisical,
its managed Secret, other users, dashboards and other databases are not changed.
The command does not synchronize or rotate the bootstrap credential afterward.
Use `--help` for both commands without connecting to the cluster.

Both commands require stdin/stdout/stderr to reference the same terminal;
pipes and redirection fail before accessing Kubernetes. They expose no new
bridge, HTTP or MCP operation, credential setup, permission grant, clipboard
action or unattended startup behavior. Dependencies are unchanged. Error
messages and child-process output are sanitized. Unit tests use dummy credentials;
the terminal integration test exercises hidden input, cancellation, echo
restoration and redirected-output rejection with fake kubectl.

After successful deployment and integration checks, follow the project [image retention and completion rule](../docs/deployment-completion.md).
