# MCP gap verification — 2026-10-14

## Scope and evidence

Baseline v0.11.1 source `94c07cbef49b356ab4344ab670fb8aeddb875ccd` matched
local/remote main, installed executable and the actual restarted Codex tool.
Its completed release evidence is retained under `outputs/releases/v0.11.1`.
This task's sanitized evidence is under `outputs/mcp-gaps` (local ignored
artifacts). Prior successful CRUD/deploy/cancel checks remain prior evidence;
contract fixtures and tool discovery do not prove live administrative permission.

## Remaining-item matrix

| Item | State | Evidence / limit |
|---|---|---|
| MCP secret value verification | Corrected; live candidate passed | New `infisical_verify_secret_write`: create match, mismatch rejection, update match, cleanup/absence. Fresh stdio uses exact Codex registration arguments/environment. Released installation and current-chat reload are separate final gates. |
| Metadata-only secret.get | Passed | Existing contract retained; value-read query is private to the generated verifier fixture. |
| Policy/read-only/path/context enforcement | Passed code tests | Real isolated policy fixtures deny before upstream effects; active policy is unchanged. |
| Error classification | Passed code/MCP transport tests | Policy denial distinct from upstream auth/permission/schema/availability; sanitized errors. |
| Environment create/update/delete | Passed live MCP v0.11.1 | Platform fixture ID `a5c2fd60-9ce4-4db0-be7a-bc4ef669df26`, unique slug `mcp-verify-6a029f`; update observed, deleted, list absence confirmed. Existing dev unchanged. |
| Role create/update/delete | Passed live MCP v0.11.1 | Unassigned Platform role ID `9bbc5963-1350-416a-b635-3a1189ea5bc4`; describeSecret only, no membership/grant; update observed, deleted, absence confirmed. |
| Project create | Deliberately restricted | Current dynamic policy denies it. No alternative credential or static-policy bypass. |
| Project rename/update/delete | Blocked live | Only Platform/Finance are visible, neither disposable; no authorized disposable project. All endpoint fixtures pass. |
| Membership add/update/delete | Blocked live | No disposable identity is exposed/provisioned by MCP. Existing identities/memberships remain untouched. Contract fixtures pass. |
| Permanent deployment | Deliberately restricted | Enablement false, profile directory unset. No unrestricted persistent opt-in. |
| Disposable deploy/cancel | Prior live pass; fresh release acceptance pending | v0.11.1 job success, digest/health and separate running-build cancellation documented. Deployment implementation is unchanged. Final local report records this task's exact binary/source and any new run. |
| Old fixture registry cleanup | Previously passed | Four exact manifests deleted with 202/404; original registry delete-disabled configuration restored. No blob GC. |
| Finance connectivity | Not tested | Explicit metadata/temporary verification does not prove application runtime connectivity, CLI/OIDC/operator delivery or real values. |

## Validation and component impact

Focused Host race tests cover Infisical, MCP access, MCP transport, Host Bridge,
enrollment/enrollment CLI and Host Agent. Full Host vet passes. All 27 existing
management operation contracts remain tested; management exposes a separate
verification tool, making 15 tools with `all/manage`, one prompt and two
resources. Doctor scopes remove the verifier. Match, mismatch, invalid schema,
permission denial, confirmation/scope denial, caller cancellation and cleanup
failure have focused tests. MCP transport tests prove value-free success and
sanitized cleanup errors.

Authorization was extracted unchanged into one shared private helper used by
ordinary commands and fixture verification. Host Bridge/Host Agent use the
shared Host module and require focused regression checks and artifact/runtime
verification. Kubernetes Agent is a separate module and has no changed source,
chart or controller; retain its pinned deployment and verify read-only relay.

## Preserved boundaries

No real Finance value, connection endpoint, environment, role or membership is
changed. Private credentials/policy, TLS, launchd and MCP registration hashes
are checked separately. Runtime/test delivery stays direct through existing
Infisical CLI/OIDC/operator; AI management uses MCP. NODE_ENV is unchanged.

No project mutation or membership pass is inferred from discovery or fixtures.
Cancellation is cooperative and does not establish rollback of arbitrary effects.
Registry manifest deletion and blob garbage collection are separate operations.
