# k6 Operator

Kubernetes-based performance-test runner with Prometheus and Grafana output.

| Field | Value |
|---|---|
| Workload namespace | `platform-tests` |
| Helm release namespace | `platform-tests` in `colima-kuchdesk-fresh` |
| UI | Provisioned Grafana dashboards |
| Metrics | Prometheus remote-write and k6 ServiceMonitor |
| Deployment | Helm chart `grafana/k6-operator` version `4.5.0` |

Durable dashboards are provisioned into Grafana. Runner resources are
temporary and must be bounded and cleaned up after a test. The included
smoke TestRun makes one request to Grafana's internal health endpoint.

Both provisioned k6 dashboards filter Prometheus remote-write results by the
operator-provided `testrun_name` label. Select **Test Run** in Grafana after a
completed run; no InfluxDB exporter is required.

The Helm release pins the k6 Operator v1.5.0 controller image by digest. The
smoke TestRun pins the runner and initializer to the k6 2.0.0
multi-architecture image digest and the starter to the matching Operator v1.5.0
starter digest. When upgrading either component, update the digests together
and verify that each image includes the cluster's architecture.

The operator runs in `platform-tests`; Prometheus must have its remote
write receiver enabled. Apply `dashboard/` in `observability` for Grafana's
dashboard sidecar. Run `kubectl apply -f tests/smoke-test.yaml` only when a new
smoke test is wanted. With `cleanup: post`, the operator removes the completed
TestRun and its jobs; capture runner logs during the run or query Prometheus
metrics afterward to verify it.

## k6 2.x runner contract

The k6 operator starts runners paused and resumes them through the k6 REST API
on port `6565`. k6 2.x disables that API by default, so every TestRun using a
k6 2.x runner must set `K6_ADDRESS=0.0.0.0:6565` under `spec.runner.env`.

## Bounded local web-dashboard acceptance

Use `tests/local-dashboard-smoke.yaml` for a single local dashboard/export
check. It is a plain Job alongside the existing operator, not a replacement
for distributed TestRuns. It makes exactly 20 GET requests with one VU and
one-second pacing to its own loopback-only synthetic sidecar. It never targets
Finance or uses credentials. All three thresholds must pass; this is not a
capacity benchmark. The Job has no retries and a 120-second deadline.

The runner remote-writes to the existing Prometheus receiver and tags metrics
with `testrun_name=<generated Job name>`. It uses scalar trend statistics,
not native histograms; select **k6 Prometheus** and statistic `p95`. The separate
native-histogram dashboard remains available for tests that explicitly use
that output. Query the run's actual time range after completion: instant rate
queries naturally become empty after their lookback window.

Start only when the fixed ConfigMap and policy are absent; `create` fails on
collision and must not be replaced with `apply` to overwrite another run:

```sh
(
  set -e -o pipefail
  for resourceKind in NetworkPolicy ConfigMap Job; do
    yq eval-all "select(.kind == \"$resourceKind\")" lab/apps/observability/k6/tests/local-dashboard-smoke.yaml |
      kubectl --context colima-kuchdesk-fresh create -f -
  done
)
kubectl --context colima-kuchdesk-fresh -n platform-tests get jobs -l kuchdesk.io/acceptance=k6-local-dashboard
# Continue only after every create succeeds; otherwise inspect partial ownership.
# Set JOB to the exact generated Job name printed by create.
JOB=k6-local-smoke-REPLACE
POD=$(kubectl --context colima-kuchdesk-fresh -n platform-tests get pods -l batch.kubernetes.io/job-name="$JOB" -o jsonpath='{.items[0].metadata.name}')
kubectl --context colima-kuchdesk-fresh -n platform-tests logs -f "$POD" -c k6
```

In a second terminal, set the same `JOB` and `POD` values, then inspect port ownership with
`lsof -nP -iTCP:5665 -sTCP:LISTEN`; do not kill an existing listener. Then run:

```sh
kubectl --context colima-kuchdesk-fresh -n platform-tests port-forward --address 127.0.0.1 "pod/$POD" 5665:5665
```

Open <http://127.0.0.1:5665/> during the run (the root redirects to its UI).
This is a temporary loopback forward, not a catalog publication or scheduler.
Do not leave a browser connected after the run: k6 can wait for live dashboard
clients. The Job deadline still bounds its total lifetime.

When logs show `KUCHDESK_K6_EXIT=0`, the shell provides a 45-second extraction
window; the k6 dashboard itself has already ended. Save outputs during that
window, before the Job's emptyDir is removed:

```sh
mkdir -p .kuchdesk/diagnostics/k6-local
kubectl --context colima-kuchdesk-fresh -n platform-tests exec "$POD" -c k6 -- cat /results/dashboard.html > .kuchdesk/diagnostics/k6-local/dashboard.html
kubectl --context colima-kuchdesk-fresh -n platform-tests exec "$POD" -c k6 -- cat /results/summary.json > .kuchdesk/diagnostics/k6-local/summary.json
kubectl --context colima-kuchdesk-fresh -n platform-tests wait --for=condition=complete "job/$JOB" --timeout=90s
kubectl --context colima-kuchdesk-fresh -n platform-tests get pod "$POD" -o jsonpath='{.status.containerStatuses[?(@.name=="k6")].state.terminated.exitCode}'
```

Stop only your forward with Ctrl-C. After checking exact names, ownership label
`kuchdesk.io/acceptance=k6-local-dashboard`, and Job UID, remove your fixtures:

```sh
kubectl --context colima-kuchdesk-fresh -n platform-tests delete "job/$JOB"
kubectl --context colima-kuchdesk-fresh -n platform-tests delete configmap/k6-local-dashboard-smoke networkpolicy/k6-local-dashboard-smoke
```

For an early stop, capture logs first and delete only that exact Job; an aborted
run is not PASS. The finite Job also has a 600-second post-completion TTL.
ConfigMap/policy cleanup remains explicit. No PVC, shared telemetry or operator
is removed. Persisted backend results and the exported HTML remain after cleanup.
The BusyBox target sidecar may be killed with137 after its five-second shutdown
grace once k6 exits; it holds no state. Report that separately from the k6 exit
code and Job result rather than calling every container a clean exit.
Prometheus currently has 3-day retention **on emptyDir**, so Pod recreation can
lose history; no disk persistence is claimed.

Grafana: <https://grafana.local.dev/d/k6-prometheus>. Select the Job name and its
time range. Use existing native Grafana login; credentials are in ignored
`lab/apps/observability/grafana/values.local.yaml` (`adminUser`, `adminPassword`).
Do not confuse this with shared panel BasicAuth or print these fields.

Offline fixture verification:

```sh
python3 -m unittest discover -s lab/scripts -p test_k6_local_smoke.py
kubectl --context colima-kuchdesk-fresh create --dry-run=server -f lab/apps/observability/k6/tests/local-dashboard-smoke.yaml -o name
```

See the dated consolidated acceptance report under
`.kuchdesk/diagnostics/grafana-k6-acceptance-2026-10-19.md` for observed results,
remaining scrape/publication gaps and the Finance read-only handoff.

## Historical namespace migration

Older installations retained Helm metadata in `observability-tests`; the fresh
cluster has both release and workload in `platform-tests`. The old
[post-renderer workflow](../../../migrations/k6-platform-tests/README.md) is
migration history, not a reason to reinstall the current operator.
