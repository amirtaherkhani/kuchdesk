# Three-day observability retention plan — 2026-10-11

**Candidate only: no live retention setting or history changed.** The user selected **three days / 72 hours**. Reviewable files are in [observability-retention](../lab/migrations/observability-retention/README.md). Active app values are untouched. No Infisical, workload data, SDK upgrade, backup or raw purge is included.

## Verified live state

Reads completed after the coordinating session recovered Docker Desktop; context remains `docker-desktop`, Kubernetes v1.36.1. Observability had 11/11 ready Pods, Prometheus 21/21 healthy scrape targets and 218 healthy rules, with aggregate rule-evaluation failures zero. These are assessment samples, not a future availability guarantee.

| Store | Installed chart / image | Current → candidate | Storage / consequence |
|---|---|---|---|
| Loki | 7.0.0 / 3.6.7 | 120h → **72h** | Bound 10Gi PVC `storage-loki-0`; eligible older log chunks can be permanently deleted |
| Tempo | 1.24.4 / 2.9.0 | 72h → **72h**, no-op | Bound 10Gi PVC `storage-tempo-0`; **do not roll out** merely to reassert this value |
| Prometheus | kube-prometheus-stack 87.15.1 / v3.13.1 | 5d → **3d** | `/prometheus` is **emptyDir**; replacement can erase **all current metrics**, including the newest samples |
| Alertmanager | Same monitoring chart / v0.33.1 | `--data.retention=120h` → **72h** | `/alertmanager` is **emptyDir**; replacement can erase **all silence and notification-deduplication state**, including active silences |
| Grafana | 12.7.3 / enterprise 13.1.0 | Optional annotation `max_age`: **72h** | Bound 10Gi PVC `grafana`; three explicit history categories below; other database content preserved |
| Alloy | 1.10.1 / v1.17.1 | No history-store setting | Forwarder without PVC; preserve positions, retries and unsent buffers |

Live Loki config uses TSDB v13 with a 24h index period, retention enabled, filesystem delete store, and `/var/loki/compactor` on the existing persistent mount. Effective compaction interval is 10m and deletion grace is 2h. Tempo uses local blocks `/var/tempo/traces` and WAL `/var/tempo/wal` on its PVC. Prometheus has no size-retention setting. Alertmanager's silence API returned **zero silences** during assessment; refresh immediately before action.

Repository sources: [Loki](../lab/apps/observability/loki/values.yaml), [Tempo](../lab/apps/observability/tempo/values.yaml), [monitoring](../lab/apps/observability/kube-prometheus-stack/values.yaml), [Grafana](../lab/apps/observability/grafana/values.homelab.yaml), [Alloy](../lab/apps/observability/alloy/values.yaml).

## History metadata, without telemetry payloads

- Prometheus: TSDB block size 961,105,509 bytes; directory 1,000,100 KiB; oldest observed TSDB timestamp 2026-10-08 16:22:01 UTC. **All of it is exposed to emptyDir rollout loss**, not just data older than three days.
- Loki: refreshed index estimate for namespace-labelled streams between 120h and 72h ago: **55,133 entries / 11,978,307 bytes**, 297 chunks / 81 streams. This is not an exact deletion count or physical disk saving: chunk overlap and streams without namespace labels limit coverage. Physical directory size was unavailable without adding tools.
- Tempo: 636 KiB, four trace-block metadata files, start/end envelope 2026-10-08 08:30:51–16:26:57 UTC. Existing 72h expiry continues normally; no additional deletion is authorized by this candidate.
- Grafana: directory 3,924 KiB. The container has no `sqlite3` executable; annotation count/oldest age could not be obtained through the available authorized paths. No database copy, new credential extraction or tool installation was attempted.

Earlier ingestion samples showed Alloy zero retries/drops and Tempo zero retention errors. Some receive/discard series were not exposed; absence is not a zero measurement. Loki, Tempo and Alloy readiness and Grafana health returned HTTP 200. Refresh metadata at action time.

## Three-day rules and dashboard alignment

The live monitoring rules include 3d burn-rate lookbacks and 30d API-server availability calculations. The candidate keeps all **133 alert rules unchanged**, including the useful 1d/3d burn alerts and their thresholds, labels and durations. It does not redefine those existing burn thresholds as a new three-day SLO policy.

A strict [post-renderer](../lab/migrations/observability-retention/post-renderer.rb) changes only the existing 16-rule `kube-apiserver-availability.rules` group: `30d` recording names/references/ranges become `3d`, and the two `* 24 * 30` day multipliers become `* 24 * 3`. One-hour dependencies, five-minute rules and histogram latency thresholds remain unchanged. The matching API-server dashboard changes those metric references and visible 30-day labels to three days. Other rules and dashboards are unchanged.

Three-day ranges fit the selected retention, but after the anticipated emptyDir reset there is **no full three-day history**. Range calculations can evaluate partial history while data accumulates; unchanged burn alerts and new availability records need warm-up. Do not present early values as a complete three-day SLO. This is a known monitoring limitation, not grounds to disable alerts.

The renderer requires the exact reviewed group/dashboard content hashes and object identities. Missing, duplicate, changed or already-transformed input fails before producing output. Re-review drift; never bypass the guards to force an upgrade.

## Cleanup behavior and preserved grace

**Loki:** retain the 24h index period, persistent marker directory, 10m compaction interval and **2h deletion delay**. Deletion is asynchronous and chunk-based; 72h is not an instantaneous disk cap. Treat existing eligible chunks as at risk: the pinned expiration implementation evaluates current retention against chunk end timestamps. A configuration revert cannot recover deleted chunks. [3.6.7 expiration logic](https://raw.githubusercontent.com/grafana/loki/v3.6.7/pkg/compactor/retention/expiration.go), [compactor configuration](https://raw.githubusercontent.com/grafana/loki/v3.6.7/pkg/compactor/config.go), [retention operations](https://grafana.com/docs/loki/latest/operations/storage/retention/).

**Tempo:** no change. Existing blocks expire according to block end time. The pinned defaults are **1h compacted-block grace** and **5m blocklist polling**, left untouched; the expanded live default endpoint was unavailable. These delays protect readers and mean physical cleanup can lag. [2.9.0 defaults](https://raw.githubusercontent.com/grafana/tempo/v2.9.0/tempodb/config.go), [deletion loop](https://raw.githubusercontent.com/grafana/tempo/v2.9.0/tempodb/retention.go).

**Prometheus:** normal retention deletes fully expired blocks asynchronously, potentially up to two hours later; head/WAL data and block boundaries prevent an exact size cap. Here the more immediate risk is Pod replacement deleting the entire ephemeral volume. Retention changes are process argument changes, not a promise of hot reload. [Storage behavior](https://prometheus.io/docs/prometheus/latest/storage/), [Operator storage](https://prometheus-operator.dev/docs/platform/storage/).

**Alertmanager:** 72h controls notification-log retention and expired-silence retention, not the lifetime of an active silence. Silence expiry is calculated after its end time; stored expiry timestamps can outlive a later setting change. Notification logs hold deduplication state, not a complete notification archive. Keep default 15m maintenance and alert routing unchanged. EmptyDir rollout can immediately lose all state and cause repeat notifications; zero silences now is not a guarantee at action time. [0.33.1 flags](https://raw.githubusercontent.com/prometheus/alertmanager/v0.33.1/cmd/alertmanager/main.go), [silence expiry](https://raw.githubusercontent.com/prometheus/alertmanager/v0.33.1/silence/silence.go), [notification-log expiry](https://raw.githubusercontent.com/prometheus/alertmanager/v0.33.1/nflog/nflog.go).

## Grafana: supported optional scope and limitations

The optional overlay sets these three **supported Grafana 13.1.0** sections to `max_age: 72h`:

- `unified_alerting.state_history.annotations`: automatic alert-state annotations, effective only for the annotations backend.
- `annotations.dashboard`: dashboard-associated annotations, including user-authored history.
- `annotations.api`: API annotations without dashboard association.

Mounted INI had no explicit annotation-age overrides. Upstream defaults retain these indefinitely and default state history to annotations. The actual runtime backend/overrides and Grafana-managed rule lookbacks still require authorized inspection. At 08:02 UTC, the Grafana rule API returned 401 and health returned 200 with database `ok`; no password was extracted or new credential created. Existing authenticated read access is needed. Default Loki notification-history support is disabled; do not enable or switch a backend merely for this request. No claim is made that all Grafana history is already covered. [Pinned defaults and supported sections](https://raw.githubusercontent.com/grafana/grafana/v13.1.0/conf/defaults.ini).

Cleanup is asynchronous; no completion SLA was established, and deleting rows need not shrink the database file. Static INI changes require restart. Preserve dashboards, users, authentication, alert definitions, contact points, deleted-rule metadata and dashboard versions. Do not apply App Platform annotation TTL settings unless that separate backend is actually enabled. The optional Grafana overlay is source-verified and offline-render verified against the local chart 12.7.3 / app 13.1.0. Baseline INI exactly matches the live ConfigMap. It remains excluded from the base rollout approval.

## Verification and remaining gates

Exact installed chart archives were fetched from official repositories and checksum-verified; no latest selection, dependency update or live Helm operation. Private live-value inputs and render outputs remain outside Git. [Render evidence and repeat commands](../lab/migrations/observability-retention/README.md).

- Monitoring: 95 objects before/after; only Prometheus retention, Alertmanager retention, one availability rule group and its dashboard differ. All 133 alert definitions are equal.
- Loki: nine objects; only configured retention and StatefulSet configuration checksum differ. Baseline parsed configuration matches the live ConfigMap.
- Tempo: four objects; **no semantic differences**. Baseline parsed configuration matches live.
- Renderer rejects missing/duplicate target objects and already-transformed content, with no output.
- Grafana: 16 objects. Parsed INI changes exactly the three `max_age: 72h` fields. Both Grafana and image-renderer Deployments change their normal config checksum because both hash the Grafana ConfigMap; both may restart. Raw offline renders also differ in the generated image-renderer Secret token: the chart uses random generation when live lookup is unavailable. A second pair with the same clearly marked **offline-only placeholder** token proves that the only nonrandom differences are the INI and those two checksums. No actual Secret was read or credential printed. Never apply these placeholder render outputs. The placeholder is neither deployment-ready nor a recovery credential. Live apply remains blocked until a supported live-aware Helm render proves the existing renderer authentication Secret stays unchanged; no actual Secret inspection was performed in this lane.

The 08:02 UTC refresh found 218 healthy Prometheus rules, including the same 133 alerts, and zero silences. The only identified Prometheus ranges beyond 72h are the two 30d recordings in the availability group covered by this candidate. These are samples; refresh immediately before action.

Before apply: refresh release/configuration and silence metadata; verify effective Loki runtime/per-tenant overrides; complete the authorized Grafana-managed alert lookback inventory. Loki's configured runtime file points to the empty `loki-runtime` ConfigMap. The current runtime endpoint returned 404, config endpoint 500 and rule endpoint 400, superseding the earlier successful rule-API sample for current access. A mounted-file read was unavailable because the Loki image lacks `cat`; no utility or helper was installed. Earlier zero loaded rules do not prove Grafana has no Loki-dependent rules. A required lookback beyond 72h is a blocker to resolve, not permission to disable its alert.

## Exact action-time approval

After preflight gates pass, obtain explicit approval for:

> Set Loki from 120h to 72h and the monitoring release's Prometheus from 5d to 3d and Alertmanager from 120h to 72h, using the same chart/image versions and the reviewed availability-rule/dashboard alignment. Eligible existing and future Loki history older than 72h may be permanently deleted. The necessary Prometheus Pod replacement can erase **all current metrics, including the most recent three days**. Alertmanager replacement can erase **all silence and notification-deduplication state**, potentially losing active silences and causing repeat notifications. Brief monitoring interruptions and history warm-up are expected. Configuration rollback cannot restore deleted history. Tempo stays unchanged at 72h; Grafana annotation expiry is separate. No backup, raw purge, Infisical or workload-data change is included.

If Grafana expiry is requested, separately name the automatic alert-state, dashboard and/or API annotation categories and permanent deletion of their history older than 72h. Do not bundle manual annotations into backend approval implicitly.

No action-time data-loss approval has been obtained in this lane. After approval, coordinate one backend at a time, check recent ingestion/query health and rule evaluation, and observe normal cleanup rather than forcing deletion. Helm 4's process-scoped subprocess plugin was successfully rendered offline: all 95 objects match the reviewed candidate after only empty/null annotation-map serialization normalization. [Descriptor and repeat command](../lab/migrations/observability-retention/README.md#helm-4-process-scoped-renderer). No global installation or live rollout was tested; live-aware preflight and the remaining runtime/access gates still apply.
