# Grafana

Grafana is the local observability UI in `observability`, reached at `grafana.local.dev` through Traefik HTTPS. The profile uses a 10 Gi PVC, Prometheus/Loki/Tempo datasources, and the pinned remote image renderer. Dashboard ConfigMaps are discovered across namespaces by the sidecar.

## Direct local values

`values.homelab.yaml` contains the hostname, TLS Secret reference, storage, datasources, and authentication-required access (anonymous access disabled). It must agree with `lab/site.json`. For a new local deployment, put fresh `adminUser`, `adminPassword`, and `imageRenderer.token` in ignored mode-0600 `values.local.yaml` using `values.local.example.yaml` for the key names. Install with both `-f values.homelab.yaml -f values.local.yaml`. The chart creates the admin and renderer Secrets directly from these values; no external secret manager is involved.

Changing this YAML does not rotate an existing Grafana database account. Existing password recovery remains an explicit Host Agent command. Do not print rendered Secrets or commit the private overlay.

## Dashboard provisioning

The homelab sidecar writes unannotated ConfigMaps under `/tmp/dashboards/Platform`. The provider scans `/tmp/dashboards` with `foldersFromFilesStructure: true`, so the Platform folder is reused. Project dashboard ConfigMaps use an absolute `grafana_folder` annotation to avoid a nested Platform folder. `allowUiUpdates: false` keeps file-provisioned dashboards source-owned.

The native sidecar collects dashboard files before Grafana starts, then watches for changes. Its `/healthz` startup probe gates the initial collection and Grafana's provider polls every 30 seconds. The sidecar does not require an admin reload credential. Changing the dashboard ConfigMaps does not require changing application credentials.

Focused chart checks run with `(cd lab && go test ./tests/grafana-auth)`. Live login, rollout, and PVC checks remain deployment-time checks.
