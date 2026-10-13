# Infisical Secrets Operator

Synchronizes Infisical secret and configuration values into Kubernetes Secrets.

| Field | Value |
|---|---|
| Namespace | `platform-secrets` |
| Helm release | `infisical-operator`, revision 5 at the completed migration |
| Chart/application version | `0.11.11` / `v0.11.11`, unchanged |
| UI | No |
| Dependency | Infisical backend |
| Observability | Controller health and metrics when supported |

The operator watches only `platform-secrets` and `observability`. Grafana uses
one v1beta1 `InfisicalStaticSecret/grafana-admin` writer and its Connection/Auth
resources in `observability`; the existing managed Secret uses Orphan retention.
The former `development-tools` scope and manager Role/RoleBinding were removed.
Both retired namespaces are absent. See the
[completed migration](../../../migrations/retired-namespaces/README.md).

## TokenRequest permission policy

The pinned local chart gives neither manager Role a TokenRequest grant:
`scripts/operator-token-scope/policy.json` contains exactly the two watched
namespaces with empty account lists. The approved reader access is separate:
`platform-secrets/Role kubedesk-infisical-token-request` and its RoleBinding let
`infisical-opera-controller-manager` request tokens only for
`platform-secrets/kubedesk-infisical-reader`. Its existing dedicated cluster
TokenReview grant is also preserved. Do not infer the reader's permission from
the empty manager-policy lists or add it to those lists as a duplicate grant.
The Infisical identity accepts this reader in `platform-secrets` only, with the
existing roles and 900-second TTL unchanged.

For offline preparation, use the official checksum-pinned archive already
verified for this version. From `lab/`, choose a new output directory:

```sh
mkdir -p .generated
python3 scripts/operator-token-scope/prepare_chart.py \
  --archive /path/to/secrets-operator-0.11.11.tgz \
  --output .generated/operator-token-scope
helm template infisical-operator .generated/operator-token-scope/secrets-operator \
  -n platform-secrets -f apps/platform/infisical-operator/values.yaml \
  -f .generated/operator-token-scope/token-request-values.json
```

The archive checksum is enforced by the offline builder. Its patch changes only
manager Role TokenRequest handling. Do not replace the local chart with a bare
upstream upgrade, which restores broad grants. A future upgrade of the existing
`infisical-operator` release requires its current complete values and resource
diff to be reviewed first. Do not blindly use `--reuse-values` to remove a
namespace: obsolete map entries can survive value merging. The completed bounded
upgrade privately preserved complete values and removed only the old watched
namespace and matching policy key. Do not replay that migration or an old Helm
rollback; historical configurations contain retired scope and reader references.

`check_permissions.py` submits authorization reviews without requesting tokens.
With the current manager policy it checks two unapproved-name denials only. It
does not prove the dedicated reader grant, TokenReview permission or old-reader
denial. Those five bounded allow/deny checks and fresh synchronization after
960 seconds passed separately during the completed rollout.

Offline regression checks include the existing pinned archive without downloading
or contacting Kubernetes:

```sh
KUCHDESK_OPERATOR_CHART=/path/to/secrets-operator-0.11.11.tgz \
  PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s scripts/operator-token-scope -v
```
