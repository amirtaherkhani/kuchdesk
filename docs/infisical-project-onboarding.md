# Infisical development project onboarding

The local Host enrollment controller discovers projects and prepares machine
memberships. Its private policy and human session stay on this Mac. The Host
identity performs MCP management; the Kubernetes identity receives only the
configured read role. The HTTPS Host Bridge keeps its separate project
allowlist and read-only API.

## Provision through MCP

An operator configures `-scope all -manage -allow-project-create` and a trusted
dynamic context with `allowProjectCreate:true`, `allowCrossProject:true`, and a
development-only `projectNamePrefix`. The Infisical machine identity must also
have upstream organization authority. Policy cannot grant that authority.

Call `infisical_manage` with `operation:project.create`, a name matching the
prefix, a unique stable `requestId`, and confirmation
`project.create:<requestId>`. Optional `slug`, `description`, `template`, and
`shouldCreateDefaultEnvs` map to native Infisical fields. Omit `slug` for the
native generated slug. Set `shouldCreateDefaultEnvs:false` to create only
native `Development/dev`; no other project's environments are changed.
Read the returned project ID, slug, and environments, then use that exact ID
for subsequent operations. A private receipt binds retries to the same
request and prevents duplicates. An uncertain outcome reports pending
reconciliation instead of trying a second create.

The controller's next bounded cycle discovers an authorized new project,
preserves existing memberships, and adds the configured Host and read-only
Kubernetes memberships when its human authority is valid. Check the actual
memberships and ledger before claiming enrollment. A project created outside
this control plane still needs upstream membership and Host policy review.
A project ID missing from machine visibility is denied by MCP even if named
in the MCP policy.

For a development-only MCP connection, set a project-name prefix and an
identity-name prefix in its trusted context, then use a nonempty `projects`
map as an allowlist. Add the target Platform project with only the operations
it needs, explicitly deny known production projects, and add a `limited` rule
for each approved development project. Rules bind the allowed operations,
environment slugs, and secret/folder path prefixes. A newly created project's
ID must receive a rule before MCP can use it; the creation prefix does not
grant follow-on access. Unlisted identity-visible projects are denied and
omitted from `project.list` whenever the map is nonempty.

## Development permissions and values

Use `role.*` and `membership.*` with exact project IDs. Disposable no-access
identities can be created and deleted only through an explicitly enabled
identity-admin context with a constrained name prefix and private ownership
receipt. Preserve real identities and memberships. Verify effective upstream
role enforcement with a disposable identity before relying on grants.

`secret.get` remains metadata-only. `infisical_read_secret_values` is the
explicit value-bearing tool for one exact project/environment/path. It excludes
imports, recursive folders, and reference expansion; a collection is sorted
and paged in memory, up to 1000 keys. Returned values enter MCP response and
conversation context. Applications and tests still obtain their own ENV
through existing Infisical CLI, OIDC, or Operator routes; an AI-triggered test
does not replace direct application secret delivery.

`infisical_group_secret_writes` first reports a value-free plan. Apply uses
explicit create/update/delete actions and a scope confirmation. Update/delete
require the version observed in the plan. Mac-local MCP sessions serialize
per scope, but Infisical v0.151.0 has no atomic write precondition against an
external actor. Group application is sequential; after a failure, inspect
per-key status and reread metadata before retrying. No omitted key is deleted.

## Deploy targets

Deployment profiles are private, bounded JSON files selected by name from the
configured absolute directory. Keep development namespace, release, source,
image repository, and context explicit. Set `KUCHDESK_DEPLOY_ENABLED=true` only
in the trusted MCP process environment. Plan and preflight precede start; start
needs exact `release/namespace` confirmation. Jobs are process-local and
cancel cooperatively, so cancellation is not an automatic rollback. Use unique
fixture names and verify cleanup of owned workloads, images, and registry
references without touching unrelated services or volumes.
