# KuchDesk Host tools on Colima/K3s

The local target is a dedicated Colima profile with containerd and K3s. Observed versions are recorded in dated diagnostics. Kubernetes uses `colima` for the default profile or `colima-NAME` for a named profile. Every build/image operation uses `colima --profile NAME nerdctl -- --namespace k8s.io`; Kubernetes and Helm use the matching explicit context. Docker tooling is not required. See [the fresh profile](../lab/README.md).

For host application and Test Runner endpoints, bridge containers, and
read-only forwarding status, follow the [local service access guide](../docs/local-service-access.md).
The source MCP tool `kuchdesk_service_access_status` runs that fixed-catalog
read-only status command. The installed binary passed an independent stdio
initialize/discovery/status check; live Codex client exposure remains unverified.
The authenticated Host Bridge `GET /v1/service-access` source uses the same
report. Its optional private-YAML `forwardingRecovery` setting runs bounded
reconciliation under the existing Bridge lifecycle, separately from the DNS
Agent. Installation/activation and live recovery remain pending; see the guide
for SSH versus gRPC/UDP support and retry limits.

The v2 source reconciler obtains its recovery-state path from the configured
root's `lab/scripts/environment_binding.py state-path` helper. Local binding
validation is required before loading or saving the persistent retry budget;
it never falls back to an empty checkout-local budget. Python status/recovery
uses the same stable private environment directory. The root still locates
installed code and catalogs; this change does not install a checkout-independent
helper. Legacy ownership fails with a migration-needed diagnostic and remains
unchanged. See [the binding and migration contract](../docs/local-service-access.md#environment-ownership-binding-source-implementation).

## Direct YAML configuration

`configyaml/` is the shared file reader/editor used by Host/MCP tools; Pods receive Helm-rendered ENV/Secrets. Its fixed catalog covers all existing chart values and the Traefik/DNS-monitoring manifests. It rejects unknown services, unsafe paths, symlink escapes and readable private files. Edits preserve unrelated keys/comments, use a cross-process nonblocking file lock and atomic replacement, and return no values. Multi-document YAML edits require a leading document index. Private/credential values are redacted from inspection.

Grafana, Technitium and Agent credentials stay in ignored mode-0600 `values.local.yaml`. Helm consumes those exact files. Changing a first-start password does not rotate a database or existing Technitium state. Fresh initialization uses a new Colima profile and new local-path storage.

## Host DNS Agent

The Host Agent is a scheduled one-shot launchd job (`RunAtLoad` and `StartInterval`), not an always-running daemon. It reads Technitium's password directly from its private YAML and uses a localhost Kubernetes port-forward for the admin API. It reconciles only the configured wildcard and skips unchanged records.

The target is the verified Colima ingress address. The Mac default-route/LAN address is never inferred as the VM address. The managed installer verifies the selected profile first, then installs `-target-ip auto`: every launchd run reads the selected Colima profile and kubeconfig, checks the API endpoint matches the VM, and uses Lima's current loopback API port. If Colima is stopped or the endpoint mismatches, that run fails without changing DNS; the next scheduled run retries. The installer pins the permanent primary checkout and kubectl path. The log directory is under `~/Library/Logs/KuchDesk/host-agent`, outside repository worktrees. Run installation from the permanent checkout after the runtime is ready:

```sh
python3 lab/scripts/install-host-services.py --bridge --dns-agent --mcp
```

Host Bridge requires `host-agent/bridge-values.local.yaml` (mode 0600) with `projectRoot` set to that same permanent checkout. Installation refuses a stale worktree path before replacing the launcher. The Bridge and certificate refresher are independent launchd jobs; no application project installs its own Host Bridge or DNS agent.

```sh
# Later, after networking and runtime authorization:
cd host-agent
go run ./cmd/kuchdesk-host-agent -kube-context colima-kuchdesk-fresh \
  -zone local.dev -target-ip VERIFIED_COLIMA_INGRESS_IP \
  -values-root /absolute/path/to/kuchdesk
```

`-check-target` validates/prints the selected target, including the current Colima address in auto mode. `-namespace`, `-service`, `-api-port`, `-ttl`, `-kubectl` and `-timeout` retain bounded controls. The chart's fixed Technitium account is `admin`. No host DNS resolver settings are changed by this CLI.

## Doctor diagnostics

Doctor checks the Mac route and DNS, selected Colima profile, its containerd k8s.io store, K3s node readiness/containerd runtime, disk, metrics and optional Deployments. Registry probing is optional and remains restricted to an explicitly supplied loopback HTTP origin. There is no required old registry address. The runtime check ID is `containerd` and Kubernetes check ID is `kubernetes`. Doctor compares the single node machine ID to the selected Colima VM. Profile readiness does not prove DNS forwarding, certificate trust or application health.

```sh
cd host-agent
go run ./cmd/kuchdesk-doctor -domain grafana.local.dev \
  -kube-context colima-kuchdesk-fresh -disk-path /absolute/path/to/kuchdesk \
  -service platform-tools/kuchdesk-agent
```

## MCP and Host Bridge

`cmd/kuchdesk-mcp` is the supported replacement; install/register it only after runtime verification under the final execution authorization. It provides Doctor, direct chart YAML catalog/read/edit, and separately enabled deployment tools. `KUCHDESK_PROJECT_ROOT` selects the canonical source root. `doctor-readonly` excludes edits/deployment; `doctor-repair` exposes typed operations, subject to their own confirmation/enablement checks. A source test or registration listing is not live MCP evidence.

The optional HTTPS Host Bridge exposes authenticated Doctor diagnostics. It has a separate bearer and TLS configuration. It does not provide an application-secret backend. `python3 lab/scripts/install-host-services.py --bridge` installs its cert-manager-issued server certificate from the existing trusted Colima CA and an owned launchd refresher that checks for CA-driven leaf renewal every 30 minutes. The refresher reads only `platform-system/kuchdesk-host-bridge-tls`, writes the private leaf/key with mode 0600, verifies both SANs and the CA chain, and reloads only the owned Bridge when the leaf changes. Kubernetes lookups derive the API port from the selected kubeconfig and use the existing Lima loopback listener with unchanged TLS and credentials; the VM/node identity check remains enforced. A natural launchd timer run and the final 30-minute launcher passed on 2026-10-20; no certificate rotation was forced. A new Mac still needs one-time OS authorization to trust a newly created development CA; the current Mac already trusts the configured CA.

## Local build and deployment workflow

`kuchdesk-deploy -profile /absolute/profile.json` prints a plan. `-preflight` runs read-only checks. `-apply -confirm release/namespace` is a live operation requiring separate authorization.

Supported profile fields are `sourceDir`, `dockerfile`, `chartDir`, `valuesFiles`, `imageRepository`, `imageTag`, `kubeContext`, `namespace`, `release`, `deployment`, required `platform`, optional `test: go`, `buildArgs`, `reuseBuild`, `rebuild`, `refreshImages`, `preflightDomain`, and optional `preflightRegistryUrl`. Removed `kindPrepull`, `pushMode` and `hostPushRepository` fields fail strict profile decoding.

For the Agent, `sourceDir` is the repository root, `dockerfile` is `kuchdesk-agent/Dockerfile`, and `chartDir` is `kuchdesk-agent/chart`. `valuesFiles` must contain its canonical base and private local YAML in that order. The Colima profile is resolved from the explicit Kubernetes context. The build context includes the shared YAML module and excludes private YAML through the root `.dockerignore`. Go tests run in the Dockerfile's module directory.

After preflight and clean-source verification, the workflow tests, builds and pushes to the explicitly selected new repository, resolves its registry digest, writes the Agent image identity into its canonical local YAML, then lints/templates/applies those same values. Helm 4 uses `--rollback-on-failure --wait`. Rollout status follows Helm; a later failure needs inspection and does not authorize destructive rollback.

Colima containerd shares images in `k8s.io` with K3s. The workflow builds directly into that store, resolves `Image.target.digest` from native nerdctl metadata, checks the requested platform, and checks complete/unpacked content through read-only `ctr images check`. No registry, push, image archive, export or node-container import is required. Helm pins the repository manifest/index digest; local images require a compatible pull policy (`IfNotPresent` or `Never`).

## Image versions

Clean source uses `git-<12-hex-source-commit>`. The current dirty-source deployment is explicitly authorized to use `git-<12-hex-HEAD>-dirty-<12-hex-build-input-hash>`. Compute it without building:

```sh
kuchdesk-deploy -identity -profile /absolute/profile.json
```

The profile initially needs a syntactically valid source tag; identity computes the current tag independently of that value. Populate its returned `imageTag` before apply. The result records the full source SHA, full input hash and platform separately from the declared application/chart version. The hash covers build-context contents, paths/modes, Dockerfile, active ignore rules, platform and explicit build arguments. Private Git-ignored files must be excluded by `.dockerignore`; included ignored files, Git metadata, symlinks, special files and unsupported ignore syntax fail closed. This helper supports simple paths, `*`, `**`, `?` and `!`; Dockerfile-specific ignore files take precedence. Inputs are rechecked before Helm so concurrent source changes block deployment.

A successful containerd inventory must establish that the immutable target tag is absent before building. Existing tags are never overwritten. Image/config IDs are not accepted in place of the containerd manifest/index digest. The workflow uses `--pull`; `rebuild`/`refreshImages` also request `--no-cache` without authorizing tag overwrite.

## Avoiding duplicate builds and pulls

`reuseBuild=true` currently returns an explicit unsupported error: containerd build/store provenance has not been implemented, so old Docker receipts cannot authorize reuse. Store locks coordinate one Host process; serialize independent CLI invocations externally. Exact publisher digest/platform/content checks allow reuse of pinned upstream images already complete and unpacked in the selected store.

The retired-image utility requires `--colima-profile NAME` and targets containerd `k8s.io`. It never exports images or assumes a Docker/node-container store. No helper performs broad prune or automatic old-image deletion. Follow [deployment completion checks](../docs/deployment-completion.md) and the project image-retention rules after verified health and recovery evidence.

## Grafana recovery commands

`grafana-admin-password --values-root /absolute/project` reveals the local bootstrap YAML value only in a verified interactive terminal after typed consent. It may differ from the current database password. `grafana-admin-password-reset --kube-context colima-NAME --user-id ID` remains an explicit interactive database recovery operation using Kubernetes exec. It does not synchronize the bootstrap YAML. Neither command is executed by startup or by these source checks.
