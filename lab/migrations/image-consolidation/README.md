# Remaining image consolidation proposals

The original proposals below are historical. The approved Loki sidecar change to pinned 2.8.1 and bounded k6 smoke run subsequently completed. Canonical Loki values now retain the sidecar digest. Today's work performed read-only recovery-source verification, with no image removal. See the [current acceptance report](../../../docs/observability-source-acceptance-2026-10-11.md) for completed actions and remaining gates; the earlier decision list is archived.

## Original Loki rules sidecar proposal

The 2026-10-11 live inventory found Loki's rules sidecar at `2.5.0` and Grafana's dashboard sidecar at `2.8.1`. Each has a live consumer, so neither version is eligible for deletion now. [loki-sidecar-values.yaml](loki-sidecar-values.yaml) proposes the existing `2.8.1` image cohort, pinned to its recorded publisher index. It does not establish compatibility with Loki's rule watcher; that needs rollout verification after approval.

Offline rendering uses the installed `loki-7.0.0.tgz`, SHA-256 `5eaa28e3535069de2a7ac1f7087ff558e5b4b4a24f0e158072a97e2391132f81`, and private live user values:

```sh
helm template loki "$ARCHIVES/loki-7.0.0.tgz" \
  -n observability --kube-version 1.36.1 \
  -f "$LOKI_LIVE_VALUES" > "$PRIVATE/loki-base.yaml"
helm template loki "$ARCHIVES/loki-7.0.0.tgz" \
  -n observability --kube-version 1.36.1 \
  -f "$LOKI_LIVE_VALUES" -f loki-sidecar-values.yaml \
  > "$PRIVATE/loki-sidecar-candidate.yaml"
```

The semantic comparison must preserve all nine object identities and every field except StatefulSet `observability/loki`, container `loki-sc-rules`, image. Keep Loki `3.6.7`, retention `120h`, PVC `storage-loki-0`, rule selectors, watcher environment, mounts, RBAC and configuration checksums unchanged. These commands render only; they do not authorize or perform deployment.

Approval would permit one Loki Pod replacement, with possible ingestion/query interruption. Before applying, refresh chart/values, compare the full candidate, and verify the pinned image's platform and recovery source. Afterward check Pod readiness, watcher errors and rule-file delivery, Loki readiness and recent ingestion/query health. Retain `2.5.0` until those checks pass. Old ControllerRevisions and exited containers may still reference it; their retirement needs a separate exact decision. Grafana stays unchanged.

## k6 helper images

The approved smoke test subsequently completed with one VU, one HTTP 200 request, all checks passing, and cleaned temporary resources. This does not change the operator's mutable defaults or authorize deletion of alternate cached helpers. The old sidecar remains referenced by rollback history. A separate source fix aligns Loki's local ruler directory with `/rules`; live deployment and file-delivery acceptance remain pending.

The installed controller is `controller-v1.5.0`, not a starter or load generator. Its [pinned starter source](https://github.com/grafana/k6-operator/blob/v1.5.0/pkg/resources/jobs/starter.go) defaults to `ghcr.io/grafana/k6-operator:latest-starter`; [runner](https://github.com/grafana/k6-operator/blob/v1.5.0/pkg/resources/jobs/runner.go) and [initializer](https://github.com/grafana/k6-operator/blob/v1.5.0/pkg/resources/jobs/initializer.go) default to `grafana/k6:latest`. Each accepts its own explicit `spec.*.image`. The installed CRD exposes all three fields. No TestRun or PrivateLoadZone existed at the audit.

The existing [smoke TestRun](../../apps/observability/k6/tests/smoke-test.yaml) already pins initializer/runner `2.0.0` and starter `starter-v1.5.0` by digest. Preserve those pins and its k6 2.x control-API address. A bounded, separately approved run can verify this cohort before retiring alternate cached helpers. Clearing a cache does not change operator defaults: another TestRun that omits the fields can fetch mutable helpers again. A global admission rule or custom operator is a separate security/development proposal.
