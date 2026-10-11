# Traefik namespace workflow

The approved 2026-10-11 cutover is complete: Helm release `traefik` revision 2 retains its metadata in `platform-system`; Deployment, ClusterIP Service and ServiceAccount `traefik` run in `platform-networking`. The existing ClusterRoleBinding `traefik-platform-system` retains its identity and role rules, with only its ServiceAccount subject namespace changed. Manual Service `traefik-lan` now belongs to the target namespace. See the [execution report](../../../docs/observability-source-acceptance-2026-10-11.md).

Use `../../scripts/helm-traefik.sh` from this directory, or `./scripts/helm-traefik.sh` from `lab`, for every normal template/upgrade command. Requirements: Helm 4, Python 3 and Mike Farah `yq`. The wrapper creates and removes a private process-scoped plugin descriptor; no global plugin is installed. Keep release name `traefik` and Helm namespace `platform-system`. The target namespace must already exist. Helm NOTES use the release namespace and do not identify the current Service DNS (`traefik.platform-networking.svc`).

The renderer is the exact reviewed cutover renderer. It accepts exactly six resources from chart 39.0.7: three namespaced objects, `ClusterRole` and `ClusterRoleBinding` named `traefik-platform-system`, and `IngressClass/traefik`. It changes only the three namespaces and the single binding subject namespace. Extra, duplicate, missing or already transformed objects, source namespace drift, role reference changes and subject changes fail before any output. Rendering does not prove all other fields match the deployed release; separately review the full candidate before a future upgrade. Chart/config changes that produce a different resource set require a fresh scope review.

From `lab`, an offline render is:

```sh
./scripts/helm-traefik.sh template traefik "$KUCHDESK_TRAEFIK_CHART_ARCHIVE" \
  -n platform-system -f core/ingress/traefik-docker-desktop-values.yaml
```

Normal upgrade commands in the [lab profile](../../README.md) use the same wrapper. A bare `helm upgrade` would revert the namespace move. Keep the manual exposure manifest in `core/exposure/docker-desktop/traefik-lan.yaml`; it preserves selectors, NodePorts 32366/32013, policies and port mapping. Do not perform another cutover or rollback solely from these examples. Historical revision 1 targets the source namespace and does not recreate the manually managed LAN Service. Any rollback needs a coordinated routing plan and verification of public HTTP/HTTPS, certificate verification, host ports and active consumers.

## Pinned validation

Supply the reviewed local `traefik-39.0.7.tgz` archive, SHA-256 `55a7384ae3a69af13e7dd7f898304a1f9890791e6712774635cd3a2df34da4c8`. The contract test verifies this checksum itself. No chart or dependency download occurs in these tests.

From the repository root:

```sh
KUCHDESK_TRAEFIK_CHART_ARCHIVE=/absolute/path/traefik-39.0.7.tgz \
  GOPROXY=off GOSUMDB=off go -C lab test ./tests/traefik-namespace -v
```

The test compares every rendered field with the unmodified pinned chart, allowing only the approved four namespace changes, checks the manual Service target/NodePorts, and exercises eight rejection cases plus wrapper ownership/command guards. A run that skips the archive-dependent tests is not acceptance evidence. The full lab suite additionally needs the Loki and monitoring archive variables documented in the [observability workflow](../observability-retention/README.md#normal-workflow-validation).
