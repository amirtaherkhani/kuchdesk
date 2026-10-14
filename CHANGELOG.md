# Changelog

All notable KuchDesk changes are documented in this file.

## [Unreleased]

## [0.13.1] - 2026-10-14

### Fixed

- Enforce the active dynamic MCP project, environment and path policy for
  synchronous and queued secret-name listing. Queued requests are checked
  again when they run; denied requests do not reach Infisical.

## [0.13.0] - 2026-10-14

### Added

- Provision development Infisical projects through the dynamic MCP policy with
  native optional metadata, generated slugs, Development-only environment
  selection, durable request IDs, and readback of actual project metadata.
- Administer disposable no-access identities, project roles, and memberships
  through scoped Host/MCP commands. Identity cleanup is restricted to private
  ownership receipts.
- Return explicitly requested secret values through a separate scoped MCP tool.
  Collections use bounded, sorted in-memory pages and exclude imports,
  recursive folders, and reference expansion.
- Plan and apply grouped secret create/update/delete operations with version
  checks, Mac-local cross-process serialization, and per-key status.
- Add a stateless development smoke source and chart for bounded deployment
  profiles.

### Changed

- Report value-read, grouped-write, identity-admin and project-creation gates
  separately from live permission and test evidence in MCP capabilities.

### Fixed

- Allow a trusted development context to request project creation without
  requiring the not-yet-created project in the identity membership listing.
  Existing project commands still intersect Host policy with current upstream
  identity visibility.
- Treat a nonempty dynamic MCP project map as an explicit allowlist, denying
  and hiding identity-visible projects that have no configured rule.

## [0.12.0] - 2026-10-14

### Added

- Add `infisical_verify_secret_write` to opted-in MCP management. It verifies
  create/update values and mismatch rejection using a new server-generated
  disposable secret, then deletes it and reports confirmed absence. Explicit
  scope, confirmation, current policy and upstream identity permissions are
  required. Existing secret names, values and value hashes are never returned.
  Failed cleanup returns the owned fixture name and a sanitized error.

## [0.11.1] - 2026-10-14

### Fixed

- Report Infisical management operations and deployment availability from the
  current MCP process configuration. Capability discovery now separates
  implemented support from exposure, permission, and live test evidence.
- Advertise the release version and clean source revision in the MCP handshake
  and capability metadata so an active connection can prove which build it loaded.
- Return a sanitized `policy_denied` result for dynamic MCP policy denials
  instead of `infisical_unavailable`. Distinguish policy, identity-list,
  upstream authentication/permission, missing-resource, and response-schema
  failures without returning targets or secret values.

## [0.11.0] - 2026-10-13

### Added

- Add an opt-in dynamic project policy for Infisical MCP management. A trusted
  startup context maps to an explicit project ID; each command intersects the
  current machine identity's project listing with Host include/exclude policy.
  Per-project deny, read-only and limited operation scopes support environment
  and path constraints. Legacy MCP scope and live `doctor-repair` configuration
  remain unchanged unless separately activated.

## [0.10.0] - 2026-10-13

### Changed

- Rename KubeDeck to KuchDesk across Go modules, binaries, chart/resources,
  environment variables, API identities, documentation and the macOS job.
  The agent chart is 0.13.0 and its app version is 0.10.0.
- Require Go 1.25 for the Host module and Go 1.26 for the Kubernetes module;
  building all modules requires Go 1.26 or newer.
- Select Host Infisical credentials explicitly: project-local `.env` by default
  or an existing macOS Keychain item. Remove global `host.env` setup and
  automatic backend fallback; credentials are not provisioned or migrated.
- Persist Grafana's completed homelab native-auth handoff with v1beta1
  Connection/Auth/StaticSecret resources, the existing
  `platform-secrets/kubedesk-infisical-reader`, one writer and Orphan retention
  for `grafana-admin`. Portable development and explicit Universal Auth remain.
- Limit Operator source scope to `platform-secrets` and `observability`; remove
  retired `development-tools` manager RBAC and policy entries. Empty manager
  TokenRequest lists do not replace the dedicated reader's limited grants.
- Preserve agent, k6 and Traefik namespace routing through scoped renderers,
  retaining existing Helm records. Record the completed retired-namespace
  migration and its guarded recovery constraints.
- Keep development dashboards in Platform with stable UIDs and source ownership.
  Use a native sidecar to collect files before Grafana starts, then watch updates
  for its 30-second file poll without admin reload credentials.
- Preserve homelab anonymous Editor access, Loki 72h, Prometheus 3d,
  Alertmanager 72h and Tempo's existing 72h retention. Use the scoped monitoring
  rule renderer and the pinned Loki chart's supported sidecar schema.
- Add optional `tls.secretName`, consistently used by leaf Certificates,
  TLSStore, application overlays and summary. The fresh-install profile selects
  `kubedesk-local-tls`; omitted/empty fields retain domain-derived names and
  explicit names survive a domain override. The source update alone does not
  migrate other existing installations' active certificates, keys, issuers or
  TLS routes. This development cluster completed a separately approved migration
  to `kubedesk-local-tls`: its three new Certificates are Ready, trusted HTTPS
  health checks pass and the three old `local-dev-tls` pairs remain for rollback.
- Require source-traceable immutable image tags and digest-pinned deployment.
  Build/KIND reuse checks exact inputs, provenance, platform and store content;
  mutable references retain refresh behavior. Local image retirement requires
  ownership, unused-reference and verified recovery checks.
- Configure Host DNS TTL, timeout, polling interval, zone, context and interface;
  pin the installer-selected `kubectl` path.

### Added

- Add official browser Admin login through `./infisical-login` and Host
  `admin-login`, automatic macOS opening, private callbacks, hidden
  Copy-to-clipboard recovery and server-checked human authority. Enrollment
  dry runs report authority as unchecked; live reconciliation follows an
  explicit project-access policy.
- Add shared Infisical CLI/MCP commands, name-only reads, project-access
  diagnostics, in-memory short-lived token renewal and bounded asynchronous
  start/status/cancel operations.
- Add an opt-in authenticated Host HTTPS bridge for Infisical metadata and
  Doctor, with project allowlists and a read-only Kubernetes proxy. Host
  administration uses scoped local CLI/MCP; bootstrap credentials stay off Pods
  and Helm values. Document explicit project onboarding.
- Add redacted Host/cluster Doctor diagnostics, typed repair-plan validation,
  MCP prompt/schema and separate readonly/repair scopes. Gate build/Helm apply
  on DNS, Docker/KIND, registry, disk and node checks with explicit preflight
  domain and registry URL.
- Add a deployment CLI and opt-in MCP plan/start/status/cancel workflow with
  source-revision validation, image build/push, digest staging, Helm apply and
  rollout checks. Add provenance-bound build reuse and per-node KIND content
  verification, plus scoped recovery helpers for retired images and Alloy.
- Add guarded metadata-only Job status, confirmed CronJob execution and Job
  deletion with UID/resourceVersion checks and dry runs. Add bounded async
  scale/restart, opt-in WebSocket Pod tunnels and noninteractive Pod exec;
  exec stays disabled by default.
- Add site rendering for DNS/TLS and an existing external issuer, plus profile
  snapshot/validation/diff/dry-run, revision-confirmed apply, private rollback
  and concurrent-write protection. Live infrastructure follows explicit workflows.
- Add macOS host metrics, Technitium site/recursion DNS probes and the Host and
  DNS dashboard; queries follow the rendered site domain.
- Add offline checksum-pinned Operator scope preparation and permission checks,
  and an opt-in Infisical-to-Helm probe without secret values in Helm inputs.
  Its historical namespace is outside the current Operator scope.
- Add interactive Grafana bootstrap-password retrieval and guarded database
  password reset, plus an anonymous dashboard helper that reconciles Platform
  resources while preserving omitted fields and server normalization.

### Fixed

- Reload enrollment interval waits without restarting, preserving elapsed time,
  retry backoff and server Retry-After limits. Omit JSON content type on bodyless
  Infisical auth requests and keep browser recovery within verified authority.
- Push images through the Host's local `crane` registry route, stage digests in
  KIND containerd and register/pre-pull through CRI before Never-policy rollouts.
  Restore missing per-node content before reuse.
- Resolve the capability catalog from rules review with bounded access-review
  fallback, and document the chart's actual RBAC and CoreDNS settings.
- Manage only a marked block of the existing CoreDNS Corefile with dry runs,
  optimistic concurrency and refusal of unmanaged rewrites.
- Use the default StorageClass for fresh Infisical PostgreSQL installations,
  preserving existing StatefulSet/PVC templates.
- Support explicit local kubeconfig contexts and multi-file `KUBECONFIG`;
  handle Ingress resource backends without crashing resource snapshots.
- Refuse ambiguous private Host addresses and use an explicit physical
  interface when a VPN owns the default route.
- Render a matching leaf Certificate for the fallback TLSStore namespace.
- Preserve dashboard schema/default fields and ignore storage metadata on
  reconcile; wait for initial file collection before starting Grafana.

### Breaking Changes

- DNS writes target the existing `kube-system/coredns` Corefile. Replace legacy
  custom ConfigMap/override settings with `KUCHDESK_COREDNS_CONFIGMAP` and
  `KUCHDESK_COREDNS_COREFILE_KEY`; obsolete Helm `dnsManagement.overrideKey` and
  `createConfigMap` values are rejected.
- KuchDesk module/chart/resource names, `KUCHDESK_*`, `kuchdesk.io/v1alpha1`
  and `X-KuchDesk-Confirm` replace KubeDeck equivalents. Upgrade client
  integrations, agent references and the renamed authentication Secret together.
- The macOS job/binary become `dev.kuchdesk.host-agent` and
  `kuchdesk-host-agent`; installation does not remove the old job automatically.
- The development dashboard watcher requires Kubernetes native-sidecar support.

### Migration

- Back up CoreDNS and review a dry run before enabling aliases. Do not reapply
  changed PostgreSQL claim templates to an existing StatefulSet; preserve PVCs
  and use a separate storage migration.
- Migrate the renamed chart/release and Secret explicitly; keep old resources
  until their replacements are verified. Stop the old macOS job before
  installing its replacement with the same zone, context and interface.
- Existing installations without a completed approved TLS migration must retain
  their current TLS name explicitly before using the updated profile, for example
  `tls.secretName: "local-dev-tls"`. A live name migration, key issuance/copying
  or old-resource retirement needs separate approved scope.
- Other installations must supply their own native-auth identity/reader. Do not
  repeat the completed alpha writer handoff, restore retired namespaces, or
  use historical Helm revisions as a blind rollback.
- Probe namespace/access and shared database/messaging proposals remain separate
  setup decisions. Image cleanup must preserve referenced or unique content
  until recovery or exact discard approval is established.

## [0.9.0] - 2026-10-09

### Changed

- Require a configured cluster ID and name for direct Kubernetes agent runs, matching the Helm chart's explicit identity requirement.
- Derive the agent's HTTP listener port from the chart Service target port; the chart version is 0.6.0 and the agent version is 0.3.0.
- Make the host DNS CLI usable on Linux with an OS-specific default-route adapter while keeping the macOS installer separate. The installer now requires an explicit DNS zone and stores absolute kubeconfig paths without Homebrew-specific PATH assumptions.
- Use cluster-domain-independent Service names for observability connections and configure Grafana's Infisical endpoint and credential location through chart values.

### Breaking Changes

- Direct Kubernetes agent invocations must set `KUBEDECK_CLUSTER_ID` and `KUBEDECK_CLUSTER_NAME`.
- Chart users must replace `agent.listenAddress` with `agent.listenHost` and `service.targetPort`.
- macOS host-agent installations must set `KUBEDECK_HOST_AGENT_ZONE` explicitly.

### Migration

- Preserve the previous cluster identity when upgrading an existing agent. Reapply the previous HTTP port through `service.targetPort` if it differed from 8080.
- Set the existing DNS zone explicitly before rerunning the macOS installer. The installer and this release do not change any running agent or DNS configuration automatically.

## [0.8.0] - 2026-10-09

### Added

- Configure the macOS host agent's DNS zone, Kubernetes context, Technitium resource names, and optional private target IP without editing source. The installer pins the selected context and preserves a custom kubeconfig.

### Changed

- Remove the `homelab` cluster identity from Kubernetes agent chart defaults. Each installation now declares its own cluster ID and name; the chart version is 0.5.0.
- Identify `lab/` as a local deployment profile rather than a requirement of either agent.

### Breaking Changes

- The host-agent CLI now requires `-zone` and `-kube-context`. The Kubernetes agent chart requires explicit `cluster.id` and `cluster.name` values.

### Migration

- Reinstall the macOS LaunchAgent with `host-agent/install-macos.sh` when ready to adopt the new binary and pinned context. Keep the existing DNS zone and context values. For an existing chart installation, pass its previous cluster ID and name during the next upgrade to preserve identity.

## [0.7.0] - 2026-10-08

### Added

- Restore the pinned k6 Operator, Grafana k6 dashboards, and a bounded one-iteration smoke TestRun in the existing `observability-tests` namespace.

### Changed

- Document the operator's Prometheus remote-write and Grafana dashboard dependencies alongside the deployment order.

## [0.6.0] - 2026-10-08

### Added

- Restore the Grafana and image-renderer chart, Prometheus Stack, Loki, Tempo, Alloy, and their Infisical, TLS, and ingress deployment configuration after these services were explicitly retained.

### Changed

- Keep Loki's existing PVC when its StatefulSet is scaled down or removed. Document the pinned observability deployment order and the required Infisical scope override.

## [0.5.0] - 2026-10-08

### Removed

- Remove the home-lab service catalog, deployment scripts, shared-service charts, local environment connector, and standalone contract/template documentation from the repository. The source tree now contains only the Kubernetes and macOS agents, their documentation, and the Kubernetes agent chart.
- Remove the chart's InfisicalSecret resource and implicit local-registry image reference.

### Changed

- Consolidate duplicate Kubernetes agent charts into `kubedeck-agent/chart` and keep catalog parser fixtures beside the agent tests.
- Require an explicit image repository and immutable tag when rendering the agent chart. The bearer Secret is provisioned separately in the target namespace.

### Migration

- Point Helm commands at `kubedeck-agent/chart`, provide `image.repository` and `image.tag`, and create `kubedeck-agent-auth` before deploying. Existing shared services are no longer managed from this repository.

## [0.4.0] - 2026-10-08

### Removed

- Remove the KubeDeck web UI, its API proxy, database, Node build chain, static assets, dashboard Helm charts, and combined UI/agent deployment scripts. The repository now keeps the Kubernetes and macOS agents without a dashboard.

### Changed

- Keep the cluster agent's API, auth Secret, service-module contract, and Helm charts independent of a dashboard. Operators who enable the agent NetworkPolicy must now select authorized client Pods explicitly.

### Migration

- Existing dashboard Helm releases should be uninstalled separately. The dashboard administrator Secret and UI PVC are not needed by either agent; retain or delete UI data according to the operator's data-retention decision.

## [0.3.4] - 2026-10-08

### Removed

- Remove unused D1 notes examples, archived release-note files, the empty Grafana dashboard placeholder, the completed NATS HA migration helper, and the optional k6 live-dashboard TestRun example.

## [0.3.3] - 2026-10-08

### Fixed

- Pin the k6 Operator controller image by digest so a fresh Helm deployment uses the same controller binary as the TestRun examples.

## [0.3.2] - 2026-10-08

### Fixed

- Pin the k6 smoke and live dashboard TestRun job images by digest so reruns use the same k6 and Operator starter versions.

## [0.3.1] - 2026-10-08

### Fixed

- Keep the Technitium admin page and local image registry available on fixed Mac localhost ports 5380 and 5001 across pod restarts and user logins.
- Reuse the persistent registry listener when preparing Docker Desktop images.

## [0.2.0] - 2026-10-05

### Added

- Add an opt-in, bearer-authenticated Kubernetes management API for discovery,
  bounded resource reads and writes, watches, pod logs, events, and workload
  scale, restart, and status actions.
- Add an authenticated dashboard management proxy and chart configuration.
  Resource writes require identity checks, version preconditions, and explicit
  confirmation; generic Secret access remains excluded.


## [0.1.4] - 2026-10-05

### Added

- Add an opt-in cluster-admin binding for the KubeDeck agent ServiceAccount,
  with chart guards requiring a bearer token Secret, RBAC creation, and a
  dedicated ServiceAccount.
- Document Docker Desktop KIND deployment constraints and the optional
  Metrics API dependency.


## [0.1.3] - 2026-10-05

### Changed

- Refresh the dashboard visual style and KubeDeck brand, including a static
  purple gradient with grain texture and a simplified logo mark.
- Align the dashboard package and both Helm charts at version `0.1.3`.

## [0.1.2] - 2026-10-03

### Added

- Add versioned ServiceModule and InstallationProfile schemas, examples, a
  validator, and shared Go and TypeScript catalog types.
- Enable first administrator setup in local development while retaining
  verified workspace identity checks for hosted setup.
- Add reduced-motion-aware transitions to shared UI components and an
  interactive dashboard banner with API and data layers.

### Changed

- Carry forward Infisical-backed dashboard and agent deployment configuration.
- Align the dashboard package and both Helm charts at version `0.1.2`.

## [0.1.1] - 2026-08-01

### Fixed

- Forward the in-cluster agent URL and bearer token into the Wrangler Worker
  runtime so authenticated dashboard snapshot, SSE, and DNS proxy routes can
  reach `kubedeck-agent`.
- Preserve empty service endpoint arrays in the agent contract and tolerate
  older snapshots that omitted them, so live catalog routes render reliably.

### Changed

- Keep the npm package, dashboard Helm chart, agent Helm chart, and Kubernetes
  client user-agent on version `0.1.1`.
- Add an authenticated post-deployment dashboard-to-agent snapshot gate to the
  Rancher Desktop release workflow.
- Document the verified immutable build and agent-first Helm deployment flow.

[0.1.1]: https://github.com/amirtaherkhani/kuchdesk/releases/tag/v0.1.1
[0.1.2]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.1.1...v0.1.2
[0.1.3]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.1.2...v0.1.3
[0.1.4]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.1.3...v0.1.4
[0.2.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.1.4...v0.2.0
[0.4.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.3.4...v0.4.0
[0.5.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.4.0...v0.5.0
[0.6.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.5.0...v0.6.0
[0.7.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.6.0...v0.7.0
[0.8.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.7.0...v0.8.0
[0.9.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.8.0...v0.9.0
[0.10.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.9.0...v0.10.0
[Unreleased]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.13.0...main
[0.13.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/amirtaherkhani/kuchdesk/compare/v0.11.1...v0.12.0
