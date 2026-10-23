# Retired namespace migration completed — 2026-10-12

This records completed runtime work; it is not a deployment or deletion recipe.
The accompanying source changes persist that verified configuration. No TLS
naming migration, new credential, image upgrade or new access grant is included.

| Component | Verified completed state |
|---|---|
| Agent and k6 | Helm history and ownership moved to `platform-tools` and `platform-tests`; active workloads stayed Ready. |
| Grafana | Release `grafana` in `observability`, revision 7; sole v1beta1 writer; managed `grafana-admin` Secret retained its UID and data; 29 dashboards and 3 datasources preserved. |
| Reader | Existing identity and limited roles retained; reader is `platform-secrets/kubedesk-infisical-reader`; Infisical namespace allowlist is `platform-secrets` only. Old reader/RBAC removed. |
| Operator | Release `infisical-operator` in `platform-secrets`, revision 5; watches `platform-secrets,observability`; removed only the old namespace's manager Role/RoleBinding. Version, resolved image and 19 retained resources including 7 CRDs preserved. |
| Retired namespaces | `development-tools` and `observability-tests` normally deleted; independent verification found both absent and no persistent leftovers. |

After the new operator Pod replaced the previous one, successful reader sync at
05:22:17 UTC was 960 seconds after the strict first-success anchor. Across the
renewal check the same Pod/image had zero restarts; reader/Auth/managed-Secret
identities, workload health and Bound PVCs were preserved. Five authorization
reviews separately verified the replacement reader's limited TokenRequest and
TokenReview access, with old-reader/default-account requests denied.

Cleanup completed at 05:34:17 UTC. Independent GET-only verification at
05:36:15 UTC checked both namespaces at the start and end and 82 persistent
namespaced API resource types: zero leftovers, seven healthy workloads, retained
Secret/reader identities and unchanged PVC identities/volumes. Old duplicate
agent credentials were removed only after exact private equality checks. The
old namespace leaf TLS certificates/keys were discarded under explicit approval;
active TLS, CA/issuer, Infisical data and credentials remained unchanged. No force
delete or finalizer removal was used. The expected owned-CertificateRequest
404 race was resolved by a separately reviewed exact-state continuation.

The [Grafana profile](../../apps/observability/grafana/README.md) records native
auth for this homelab; portable development and explicit Universal Auth remain
supported. The retired operator policy
records empty manager TokenRequest grants; the dedicated live reader grant is
separate and is not recreated by this source reconciliation.

## Guarded recovery

Retained Helm history and historical ReplicaSets are evidence, not a safe
one-command rollback. An older Grafana revision can restore the alpha writer;
an older operator revision can restore the retired watch scope and access.
Before recovery, review complete current values, ownership and exact resource
diffs; preserve the sole writer, managed Secret UID/data, reader permissions,
PVCs, active TLS and Infisical identity. Do not recreate retired namespaces,
blindly roll back Helm, blanket-adopt resources, or re-run the completed orphan
handoff. Any recovery that changes security or discards data needs its own exact
approval.

## Sanitized evidence

These existing receipts remain in the task workspace rather than being vendored
into the repository. Their SHA-256 values identify the records used for this
completion note; none contains credential values or Helm payloads.

| Receipt | SHA-256 |
|---|---|
| `grafana-helm-reconcile-result.json` | `a0b491dd011d618d11970d56d69114e5a467acd893452ed5768d180157fce62e` |
| `guarded-upgrade-result.json` | `c4088a92dbe190fb6948999328eed906abe9bd303789714c025a1ad4d7a56de1` |
| `operator-scope-renewal.json` | `96dfd8c4d4df7f5933db34103b94489c4bdb7ef18caf4f7233b0d6b4e6beaa96` |
| `post-cleanup-independent-verification.json` | `00974c7223c2c8ef1b6c71475075122f402451f8e12d6edc9304a274f0506829` |
