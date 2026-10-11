# Grafana

Primary home-lab observability UI.

| Field | Value |
|---|---|
| Namespace | `observability` |
| UI | Yes: generated `grafanaUrl` from `lab/site.json` |
| Port | HTTPS through Traefik; service `3000` |
| Storage | `standard` PVC, 10 Gi |
| Datasources | Prometheus, Loki, Tempo, Alertmanager |
| Dashboards | Kubernetes, macOS, platform, and service dashboards |
| Rendering | Remote image renderer `v5.12.2` for panel/dashboard exports |

Dashboard ConfigMaps are discovered across namespaces using the Grafana sidecar.
Stable datasource UIDs are `prometheus`, `loki`, `tempo`, and `alertmanager`.

The Platform / Observability dashboard limits its Kubernetes log pod filter to
pods currently reported by Prometheus as `Running`; Loki history is retained
for the configured backend retention period.

The local Grafana admin credential is stored in Infisical/Kubernetes Secret and
must never be committed to this repository. Supply the active Infisical project
and environment with `--set-string infisical.projectSlug=...` and
`--set-string infisical.envSlug=...` when deploying this chart.

Native Kubernetes auth is opt-in via `infisical.authMethod=kubernetesAuth`. Set
`infisical.kubernetesAuth.identityId` to the approved identity UUID and provide
`serviceAccountRef.name` and `.namespace` under the same key. The Operator requests
short-lived tokens; no Universal Auth credential reference is rendered in this
mode. The default remains `universalAuth`. Unknown modes or missing native fields
fail rendering. Prepare the service account, TokenRequest/TokenReview permissions
and Infisical auth configuration from the [activation plan](../../../../docs/infisical-activation-plan.md)
before applying. Template tests verify auth changes do not change either Grafana
Deployment; keep the existing managed Secret during cutover. The checked-in
homelab environment now matches the verified live `dev` scope.

## Durable development dashboard access

`values.homelab.yaml` keeps the already-approved anonymous `Editor` role.
The dashboard sidecar now writes unannotated ConfigMaps into
`/tmp/dashboards/Platform`. The file provider scans `/tmp/dashboards` with
`foldersFromFilesStructure: true`, so Grafana creates/reuses the **Platform**
folder rather than treating that directory's files as General dashboards.
The project dashboard ConfigMaps use the absolute `grafana_folder` annotation
`/tmp/dashboards/Platform` to avoid nesting Platform twice. Their JSON content
and UIDs are unchanged. Other explicit absolute folder annotations still work.
Relative annotations are interpreted relative to the sidecar's default folder.

On the next rollout, unannotated file-provisioned dashboards will be placed in
Platform; their content/UIDs remain source-owned (`allowUiUpdates: false`).
Apply the dashboard Kustomization before changing the Grafana sidecar directory
so the two project ConfigMaps already have their absolute target annotation.
No startup password repair, ACL API, anonymous Admin, database deletion or
Infisical change is required. File provisioning recreates/reuses folders when
dashboard sources exist; it does not restore arbitrary user-edited database data.

For another development environment, layer `values.development.yaml` with that
environment's existing site/Infisical overlay. The shared profile stores the
same anonymous role, bootstrap Secret mapping and folder behavior without
hardcoding an Infisical project, environment, host or credential. The chart
requires those selectors in the environment overlay. Use this profile only
where anonymous development editing is intended.

For ordinary dashboards created through the resource API, use the repository
helper instead of a raw create request with no folder annotation:

```sh
python3 lab/scripts/grafana-dashboard.py \
  --url https://grafana.local.dev --file /path/to/dashboard.json
```

It accepts exported dashboard JSON with a stable UID or a `metadata`/`spec`
resource. It resolves the existing Platform UID by title, creates that folder
idempotently if absent using Grafana's ordinary folder defaults, and defaults
`metadata.annotations["grafana.app/folder"]` to the resolved UID. Reapplying the
same dashboard skips the write; edits use the current resource version. It
refuses to move existing General/other-folder dashboards, override another
folder annotation, or update source-provisioned dashboards. Requests are
anonymous with trusted TLS; there is no admin-credential or ACL fallback. A
denied request stops and reports failure. Raw third-party API creates without
an annotation remain outside this supported path.

The 2026-10-11 live diagnosis found an empty ACL on the temporary dashboard
created in General. Anonymous create/read/edit in the existing Platform folder
returned 201/200/200; the 29 existing dashboard bodies were unchanged and both
temporary fixtures were removed after restoration checks. These results verify
the current deployment's behavior, not a fresh installation of the new source.
The new chart/provider changes have been render-tested, not rolled out; no live
database was wiped for validation.

Regression checks:

```sh
(cd lab && go test ./...)
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s lab/scripts -p 'test_grafana_dashboard*.py'
```

The opt-in acceptance test is skipped by default. Against an explicitly chosen
development/test deployment, set `KUCHDESK_GRAFANA_ACCEPTANCE_URL` and run
`test_grafana_dashboard_live.py` with unittest discovery. It creates one unique
temporary dashboard, checks anonymous read/edit/idempotent reapply, and removes
only that fixture. Run it after a fresh install or approved rollout to validate
the real API and inherited folder permissions; this test has not been run
against a fresh database in this change.

References: [Grafana file provisioning](https://grafana.com/docs/grafana/latest/administration/provisioning/#dashboards)
and [sidecar folder annotation behavior at 2.8.1](https://github.com/kiwigrid/k8s-sidecar/blob/2.8.1/src/resources.py#L87).
