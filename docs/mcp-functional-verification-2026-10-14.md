# MCP functional verification — 2026-10-14

## Checklist and evidence

- [x] Baseline: `main` and `origin/main` both `05f599e8939a6662e82745554c00f8ad9b5e564f` before work; one clean task worktree. Installed `kuchdesk-doctor` binary SHA-256 was `1d5f8131155a98f5850462accac80269136ffea2d4f64d54a464b306c95d5112`, the reported v0.11.0 artifact.
- [x] Capabilities: updated binary, fresh stdio process, 14 tools; `configured=true`, `management.exposed=true`, `policyMode=dynamic`, 27 implemented operations, `projectCreationEnabled=false`, `permissionStatus=not_checked`, `deployment.enabled=false`, `profileAvailable=false`, `testCoverage=not_asserted_by_discovery`.
- [x] Error boundaries: table tests cover policy denial/unavailable, identity listing, upstream 401/403/404/400, invalid schema, and transport fallback. MCP transport test confirms `policy_denied` without target disclosure. Isolated dynamic-policy tests confirm allowed read and limited-path cases and deny read-only writes, denied projects, wrong environment/path, and project creation before upstream effects.
- [x] Secret values: opt-in live fixture in Finance project `fd27d482-48bc-4b6c-b194-5901bdbc497b`, `dev`, `/`. `CODEX_MCP_FIXTURE_a3381a1f9ea978355b7464836a5d815dd3ecb3718c932f0d` was created, compared internally after create and update, deleted, and confirmed absent. The MCP write contract does not return upstream secret IDs, so the generated name is the cleanup identifier. No marker value was logged or returned. Ordinary `secret.get` still requests `viewSecretValue=false` and returns metadata only.
- [x] Management contracts: all 27 supported operations have isolated endpoint/confirmation fixtures. Earlier reported live MCP secret/folder writes are retained as prior evidence; this run did not repeat unrelated live writes. Project, environment, role, and membership writes remain **live unverified** under the current policy and were not attempted on real Finance resources.
- [x] Deployment: updated source binary SHA-256 `445eb64c15f3e7413cd15988db3473885bc74c883ee95f718b766638094f4488` drove stdio plan, ready preflight, start, and job status for stateless release `codex-mcp-validation-iqz0k4` in `platform-tests` (job `d02a51d08ab86aa098f9cd21d43ed621`). Fixture source `4f4b3e6dcc613202b2a02e3508c906168eebe72a`, image tag `git-4f4b3e6dcc61`, registry digest `sha256:e4432a5bcedf9b4364b215fde75b77bf6eaafc8964d494779d17ef25c91cd2eb`, `linux/arm64`. Helm revision 1 deployed; 1/1 ready; pod image ID matched digest; HTTP body was `KuchDesk MCP fixture ready`; logs were empty. CLI/build work occurred inside MCP deploy start, not outside it. The later source edits affect policy error mapping and Doctor-scope tool removal; the final binary was rebuilt and passed a fresh capability check but did not repeat the rollout.
- [x] Separate cancellation: test-owned release `codex-mcp-cancel-iqz0k4` used source `73bddd7619359fbcb3c2d2cbc29feb335ebe2a34` with a bounded slow Docker build. MCP job `fca443ac1069e9c12dc03d3afce1ccab` reached `running` with its build process observed; `kuchdesk_cancel_job` led to final `canceled`. No Helm release, Kubernetes workload, registry repository, or completed Docker image existed afterward. Cancellation is cooperative and does not promise rollback of arbitrary side effects.
- [x] Cleanup: successful fixture Helm release and Pods removed; no test-owned workload remains. Exact Host Docker image `sha256:ba808aec41df5bb3ab37d1ee74185a5f6f91900e8759283f2c286a63d817707c` and KIND CRI image `sha256:9ae090eb1e0275d32fc71f49dab7d05bd3d90e8cf0f257266ea5b5af5bf94eea` were removed without force after checking containers and desired workloads. The pinned registry digest was verified pullable before removal and remains available. Temporary process-only deployment settings did not change the installed registration.
- [x] Validation: Host `go test ./...`, focused race tests, `go vet ./...`, diff check, live secret fixture, stdio capability check, and live stateless deploy/cancel checks passed. The final temporary binary SHA-256 is `929a09cccca152c01ce4493cc3e863313329ebaa9629e2aa83b009a6cb8d1bdb`. No Finance role, membership, real secret value, environment, or active service configuration was changed.

## Exposed tools

`E` means exposed by the current all-scope registration. `L` means live-tested in this run; `F` means deterministic fixture-tested. Tool discovery alone is not a permission test.

| Tool | Exposure / result | Coverage |
|---|---|---|
| `infisical_capabilities` | E, passed | L + F |
| `infisical_list_secret_names` | E, conditional upstream access | F; no fresh live run |
| `infisical_start_list_secret_names` | E, conditional upstream access | F; no fresh live run |
| `infisical_job_status` | E, in-process only | F |
| `infisical_cancel_job` | E, in-process only | F; not used as deployment proof |
| `infisical_manage` | E, dynamic policy; per-command authorization | F all operations through the command boundary; live adapter secret fixture in this run; earlier live MCP writes reported |
| `kuchdesk_deploy_plan` | E, named profile required | L passed |
| `kuchdesk_deploy_preflight` | E, named profile required | L passed |
| `kuchdesk_deploy_start` | E, currently disabled in installed registration | L passed in isolated opt-in process |
| `kuchdesk_job_status` | E, in-process only | L passed |
| `kuchdesk_cancel_job` | E, cooperative cancellation | L passed on separate build job |
| `kuchdesk_doctor` | E, read-only | F; no fresh live run needed |
| `kuchdesk_doctor_validate_plan` | E, bounded plan validation | F |
| `kuchdesk_doctor_verify` | E, read-only check rerun | F |

The registration also exposes one Doctor prompt and two Doctor resources. These were not changed.

## Management operation matrix

All 27 operations passed isolated request/endpoint fixtures. `Conditional` means the dynamic policy can permit the operation on an identity-visible, Host-allowed project, while the upstream permission remains separately checked. `Denied` is the current dynamic-policy result. A write fixture is not live upstream write evidence.

| Operation | Current policy | Live evidence | Isolated result |
|---|---|---|---|
| `project.list` | Conditional | No operation-level proof in supplied summary | Passed |
| `project.get` | Conditional | No operation-level proof in supplied summary | Passed |
| `project.create` | Denied | None; no project created | Passed static fixture; dynamic denial passed |
| `project.rename` | Conditional | None | Passed |
| `project.update` | Conditional | None | Passed |
| `project.delete` | Conditional | None | Passed |
| `environment.list` | Conditional | None | Passed |
| `environment.create` | Conditional | None | Passed |
| `environment.update` | Conditional | None | Passed |
| `environment.delete` | Conditional | None | Passed |
| `folder.list` | Conditional | No operation-level proof in supplied summary | Passed |
| `folder.create` | Conditional | Earlier Finance fixture reported | Passed |
| `folder.update` | Conditional | Earlier Finance fixture reported | Passed |
| `folder.delete` | Conditional | Earlier Finance fixture reported | Passed |
| `secret.list` | Conditional | Name checks reported; exact call evidence unavailable | Passed |
| `secret.get` | Conditional | Metadata read reported; value intentionally omitted | Passed, value redacted |
| `secret.create` | Conditional | Passed on test-owned `dev` fixture | Passed |
| `secret.update` | Conditional | Passed on test-owned `dev` fixture | Passed |
| `secret.delete` | Conditional | Passed on test-owned `dev` fixture | Passed |
| `role.list` | Conditional | None | Passed |
| `role.create` | Conditional | None | Passed |
| `role.update` | Conditional | None | Passed |
| `role.delete` | Conditional | None | Passed |
| `membership.list` | Conditional | None | Passed |
| `membership.add` | Conditional | None | Passed |
| `membership.update` | Conditional | None | Passed |
| `membership.delete` | Conditional | None | Passed |

## Registry and remaining decisions

The earlier `codex-mcp-probe-0f2b7a61` manifests are still present and **tagged**: `git-20a85dae7d00` points to `sha256:aa3018e3b8bc0a19ec46318c9e0d7a12a400c8eee151f46020fff8ead633902e`; `git-e93eefc23c0b` points to `sha256:9256d636a09b0fd1fac604ef9f7731708a1f03459a05e6c5903488db942d3dd4`. No Kubernetes workload currently references that repository. This run's `codex-mcp-validation-iqz0k4:git-4f4b3e6dcc61` manifest remains pullable by digest after local image cleanup. Registry `platform-system/kuchdesk-local-registry` has only `REGISTRY_HTTP_ADDR` set; manifest deletion is disabled by the current registry configuration. No retry of the previously unsupported delete, PVC change, direct storage deletion, or garbage collection occurred. A separate operator decision must enable targeted manifest deletion, verify references again, and schedule registry maintenance; [Distribution documentation](https://distribution.github.io/distribution/about/configuration/) distinguishes delete enablement from [blob garbage collection](https://distribution.github.io/distribution/about/garbage-collection/).

The installed binary and Codex registration were not replaced. Persistent deployment enablement/profile selection and organization-level project-creation authority remain separate opt-in decisions. No public release or tag was created.
