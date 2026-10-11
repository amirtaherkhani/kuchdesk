# Three-day retention workflow

The approved retention change completed on 2026-10-11: Loki 72h, Prometheus 3d and Alertmanager 72h. Tempo already used 72h and was left unchanged. Canonical app values now preserve these settings. See the [current acceptance report](../../../docs/observability-source-acceptance-2026-10-11.md) for earlier live evidence, current offline verification and pending live work.

Use [helm-monitoring.sh](../../scripts/helm-monitoring.sh) for normal monitoring templates and upgrades. It creates a temporary, process-scoped Helm 4 plugin for the checked-in renderer and removes the plugin directory on exit. It does not install a global plugin. Exact content hashes and identities reject missing, duplicate, changed or already-transformed targets. Re-review drift; do not bypass the guard. Earlier approval does not authorize another rollout, purge, backup or optional Grafana expiry.

## Normal workflow validation

Obtain the exact Loki 7.0.0 and kube-prometheus-stack 87.15.1 archives from the official release repositories and verify the SHA-256 values below. Tests do not fetch charts automatically. From the repository root, using absolute archive paths and a private temporary output directory:

```sh
export KUCHDESK_LOKI_CHART_ARCHIVE="$ARCHIVES/loki-7.0.0.tgz"
export KUCHDESK_MONITORING_CHART_ARCHIVE="$ARCHIVES/kube-prometheus-stack-87.15.1.tgz"
go -C lab test ./...
helm template monitoring "$KUCHDESK_MONITORING_CHART_ARCHIVE" \
  -n observability --kube-version 1.36.1 \
  -f lab/apps/observability/kube-prometheus-stack/values.yaml \
  > "$PRIVATE/monitoring-base.yaml"
python3 lab/migrations/observability-retention/test_renderer.py \
  "$PRIVATE/monitoring-base.yaml"
lab/scripts/helm-monitoring.sh template monitoring \
  "$KUCHDESK_MONITORING_CHART_ARCHIVE" \
  -n observability --kube-version 1.36.1 \
  -f lab/apps/observability/kube-prometheus-stack/values.yaml \
  > "$PRIVATE/monitoring-candidate.yaml"
```

Requirements: Go, Helm 4, Ruby with bundled YAML/JSON, and Mike Farah's `yq`. Without the archive variables, chart-dependent Go tests explicitly skip; that is not full acceptance. The renderer contract compares all objects and every alert and checks missing, duplicate, drifted and reapplied inputs. Normal-workflow tests verify retention, dashboard alignment, Loki's shared rules directory and sidecar digest, and rejection of a renderer override.

For a live release, refresh private user values and compare a live-aware render before an approved upgrade. Never commit live values or bypass Helm ownership with generic apply. The earlier retention upgrade used `--no-hooks` after verifying seven unchanged admission hooks; initial installation still requires normal admission setup. An offline render alone does not establish safe hook handling for a future upgrade.

The completed live check observed Prometheus 3.13.1 hot reload with the same Pod, loaded retention 3d and TSDB retention 259200 seconds, although its flags endpoint still reported 5d. Alertmanager was replaced with 72h and zero silences before and after. All 218 rules were healthy, all 133 alerts stayed unchanged, and all 29 dashboard UIDs were preserved. These are earlier measurements; subsequent Docker startup/readiness verification does not repeat them. Physical expiry of old Loki chunks was not proven.

## Historical candidate record

The overlay files, preflight measurements and remaining-gate wording below record the original assessment before the approved operation. They are retained as historical evidence. Optional Grafana annotation expiry remains excluded and undeployed. See the [dated assessment](../../../docs/observability-retention-plan-2026-10-11.md).

| File | Candidate |
|---|---|
| [loki-values.yaml](loki-values.yaml) | Loki 120h → 72h |
| [monitoring-values.yaml](monitoring-values.yaml) | Prometheus 5d → 3d; Alertmanager 120h → 72h |
| [post-renderer.rb](post-renderer.rb) | Required matching 30d → 3d availability records and API-server dashboard; all health alerts preserved |
| [tempo-values.yaml](tempo-values.yaml) | Documentary no-op: already 72h; **do not roll Tempo** |
| [grafana-values.optional.yaml](grafana-values.optional.yaml) | Separate optional 72h annotation expiry; offline render verified; backend/access checks still pending |

**Prometheus and Alertmanager use emptyDir.** Their Pod replacements can lose all current metrics and all silence/notification state, not only history older than three days. Loki expiry can permanently delete older log chunks. Tempo needs no change. Grafana manual annotations require explicit inclusion. Configuration rollback does not recover data.

## Pinned render evidence

Exact installed archives were downloaded from official repository release URLs, verified against index SHA-256 values, and used with private live user values and Kubernetes 1.36.1. No upgrade or live mutation occurred.

| Archive | SHA-256 |
|---|---|
| loki-7.0.0.tgz | `5eaa28e3535069de2a7ac1f7087ff558e5b4b4a24f0e158072a97e2391132f81` |
| tempo-1.24.4.tgz | `f1f6e318d5bca3b5097cb676077796cdf8135beb2c1f71c4d14614ccf9b0081b` |
| kube-prometheus-stack-87.15.1.tgz | `4334ab0443245c748d73be7d84ce447bb7f3715c63e08fecf05eca66eb9f567b` |

Semantic checks passed on 2026-10-11:

- Monitoring: same 95 objects; only `/spec/retention` in Prometheus and Alertmanager, the existing 16-rule availability group, and matching dashboard JSON change. All 133 alert rules unchanged.
- Loki: same nine objects; only `/limits_config/retention_period` in parsed ConfigMap config and StatefulSet checksum change. Persistent marker path, 10m compaction and 2h deletion delay unchanged.
- Tempo: same four objects, no differences; no rollout justified.
- Grafana local chart 12.7.3 / app 13.1.0: same 16 objects; baseline INI exactly matches live. Candidate INI changes only the three max-age fields. Both Grafana and image-renderer Deployment config checksums change. Raw offline renders additionally generate different renderer Secret tokens because Helm lookup is unavailable; a second pair using an identical **offline-only placeholder** token isolates that randomness and confirms no other differences. No actual Secret was read. Never apply those placeholder outputs; preserve the existing token with supported live chart lookup. Both Deployments may restart. The placeholder is neither deployment-ready nor a recovery credential; live apply remains blocked until a supported live-aware Helm render proves the existing renderer authentication Secret stays unchanged.
- Missing/duplicate targets and already-transformed renderer input fail with no output. Content hashes reject rule/dashboard drift before transformation.

## Repeat offline validation

Use the checksum-verified archives and freshly checked private live-value files. From this directory, with absolute `ARCHIVES`, `PRIVATE` and live-value paths:

```sh
helm template monitoring "$ARCHIVES/kube-prometheus-stack-87.15.1.tgz" \
  -n observability --kube-version 1.36.1 \
  -f "$MONITORING_LIVE_VALUES" > "$PRIVATE/monitoring-base.yaml"
python3 test_renderer.py "$PRIVATE/monitoring-base.yaml"
helm template monitoring "$ARCHIVES/kube-prometheus-stack-87.15.1.tgz" \
  -n observability --kube-version 1.36.1 \
  -f "$MONITORING_LIVE_VALUES" -f monitoring-values.yaml > "$PRIVATE/monitoring-overlay.yaml"
ruby post-renderer.rb < "$PRIVATE/monitoring-overlay.yaml" > "$PRIVATE/monitoring-candidate.yaml"
helm template loki "$ARCHIVES/loki-7.0.0.tgz" \
  -n observability --kube-version 1.36.1 \
  -f "$LOKI_LIVE_VALUES" -f loki-values.yaml > "$PRIVATE/loki-candidate.yaml"
helm template tempo "$ARCHIVES/tempo-1.24.4.tgz" \
  -n observability --kube-version 1.36.1 \
  -f "$TEMPO_LIVE_VALUES" -f tempo-values.yaml > "$PRIVATE/tempo-candidate.yaml"
```

Compare with no-overlay baseline renders by object identity and parsed contents. Keep outputs private; do not commit live values. The renderer uses Ruby's bundled YAML/JSON libraries. It is intentionally pinned to the reviewed rule/dashboard content and fails on drift or reuse on transformed input.

The [contract checks](test_renderer.py) verify unchanged objects and alert definitions, the limited recording/dashboard fields, and rejection of missing, duplicated, drifted or already-transformed inputs. Use `yq` to parse YAML for semantic comparison: Ruby-generated JSON can normalize a leading colon in unrelated metric names and is not the comparison baseline.

## Helm 4 process-scoped renderer

Helm 4 expects a postrenderer plugin name. Its subprocess plugin integration was verified offline against the same pinned chart, private live values and overlay: all 95 objects equal the reviewed candidate after normalizing only empty/null `metadata.annotations` maps omitted by serialization. No global plugin installation or live Helm operation occurred.

Create `<private-plugin-root>/monitoring-retention/plugin.yaml` with this descriptor, setting the command to the maintained checkout:

```yaml
apiVersion: v1
type: postrenderer/v1
name: monitoring-retention
version: 0.1.0
runtime: subprocess
runtimeConfig:
  platformCommand:
    - command: /absolute/repository/lab/migrations/observability-retention/post-renderer.rb
```

Repeat the actual Helm 4 integration offline:

```sh
HELM_PLUGINS="$PRIVATE_PLUGIN_ROOT" helm template monitoring \
  "$ARCHIVES/kube-prometheus-stack-87.15.1.tgz" \
  -n observability --kube-version 1.36.1 \
  -f "$MONITORING_LIVE_VALUES" -f monitoring-values.yaml \
  --post-renderer monitoring-retention > "$PRIVATE/monitoring-helm4-candidate.yaml"
```

Compare the entire result with the separately reviewed renderer output. The renderer's drift/reuse guards remain mandatory. A live upgrade must use the same process-scoped integration and reviewed inputs after the remaining gates pass; this render check is not a tested rollout. Never apply the whole rendered monitoring stack with a generic command to bypass Helm ownership.

The 08:02 UTC read found 218 healthy Prometheus rules, including 133 alerts, and zero Alertmanager silences. Grafana health returned 200 with database `ok`; its rule API returned 401. Loki readiness returned 200, but config/runtime/rule endpoints returned 500/404/400. The configured `loki-runtime` ConfigMap is empty; an attempted mounted-file read failed because the Loki container has no `cat` executable. This narrows the override inventory but does not prove loaded runtime configuration or absence of Grafana-managed rules.

Remaining gates: fresh effective Loki override/rule verification and authorized Grafana-managed rule/backend inventory; exact irreversible-history approval; refresh active silence counts and complete live-aware Helm preflight. The optional Grafana overlay additionally needs explicit category approval and verification that the live rollout preserves the existing renderer token. Preserve current versions, ingress/auth, namespaces, PVCs, ingestion and deletion grace. No new backups or raw purges are included.
