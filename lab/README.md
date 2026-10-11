# Observability runtime

This is the checked-in deployment profile. [`site.json`](site.json) contains this installation's DNS domain, service host labels, TLS issuer, and certificate namespaces. Edit it or pass another file to `go run ./cmd/render-site --profile path/to/site.json`; no domain is embedded in deployable Ingress, Certificate, TLSStore, or Helm values. The agent code has no site default.

For controlled changes to the local site profile, `render-site config` provides
`snapshot`, `validate`, `plan`, `dry-run`, `apply`, and `rollback`. Pass absolute
paths to the current profile and candidate. `plan` lists changed fields and
returns a `planId`; `apply` requires that exact ID and rejects a profile that
changed since planning. It saves a private revision backup beside the profile
before replacing the file atomically. `rollback` requires both the saved
revision and the current revision as confirmation. A lock prevents concurrent
applies. The backup and lock files are Git-ignored for the checked-in profile.

```sh
cd lab
go run ./cmd/render-site config snapshot -profile "$PWD/site.json"
go run ./cmd/render-site config validate -profile "$PWD/site.json" -candidate /absolute/site.next.json
go run ./cmd/render-site config plan -profile "$PWD/site.json" -candidate /absolute/site.next.json
go run ./cmd/render-site config dry-run -profile "$PWD/site.json" -candidate /absolute/site.next.json
go run ./cmd/render-site config apply -profile "$PWD/site.json" -candidate /absolute/site.next.json -confirm PLAN_ID
go run ./cmd/render-site config rollback -profile "$PWD/site.json" -revision SAVED_REVISION -confirm CURRENT_REVISION
```

These commands change only the selected JSON profile. They do not apply DNS,
TLS, Helm, Kubernetes, firewall, or host-network changes. After an apply or
rollback, render and review the generated files, then update live components
through their existing workflows. This is a deliberate apply boundary; there
is no automatic hot reload of all services.

For another cluster, select its Kubernetes context, DNS zone, issuer, ingress class, storage classes, Secret names, and Infisical scope. The site renderer handles domain and TLS references only; the other Helm values and PostgreSQL manifest remain site-specific overlays. The observability components use Kubernetes search-domain Service names rather than assuming `cluster.local`. Grafana's Infisical endpoint, credential Secret, and path are configurable in `values.homelab.yaml`. Existing PVC storage classes are immutable; changing storage class requires a separate data migration, never just a site render.

This directory keeps the Grafana observability stack, k6 Operator, and the platform configuration required for local HTTPS and Infisical-backed Grafana credentials. It does not manage the KuchDesk dashboard, shared application databases, or unrelated home-lab services.

The runtime on Docker Desktop Kubernetes consists of Grafana and its image renderer, Prometheus Stack, Loki, Tempo, Alloy, and k6 Operator. cert-manager and Traefik provide HTTPS for the selected site domain; Infisical and its operator supply `observability/grafana-admin`. The existing Grafana, Loki, Tempo, and Infisical PVCs are reused. Loki's chart values retain its PVC when the StatefulSet scales down or is removed.

## Prerequisites

- The `docker-desktop` Kubernetes context is selected and its node is Ready.
- DNS resolves the generated `grafanaUrl` and `infisicalUrl` hosts to the ingress address. For a private CA, trust its root certificate on the client host.
- `platform-secrets/infisical-secrets`, `platform-secrets/infisical-postgresql`, and `platform-secrets/infisical-universal-auth` exist. Provision credentials through the established secret workflow; do not place them in Helm values or Git. The Operator scope includes `development-tools` for the opt-in [Infisical-to-Helm probe](tests/infisical-helm-probe/README.md); the probe uses a dedicated Infisical path and creates no public Service.
- A new Infisical PostgreSQL installation needs a default StorageClass. The checked-in StatefulSet manifest leaves its class unset. On an existing installation, keep the bound PVC and StatefulSet template unchanged; this field is immutable and reapplying the manifest will fail.
- Set `INFISICAL_PROJECT_SLUG` and `INFISICAL_ENV_SLUG` to the active Infisical project and environment. Override the chart's sample scope for every deployment.

## Deployment order

The pinned versions below match the verified local stack. Run these commands from `lab/`.
Go 1.23 or newer is required. `--get` reads the last rendered summary from `.generated`; pass the same `--output` directory when using a separate profile output.

For a **new** PostgreSQL installation only, first check `kubectl get storageclass` and confirm one default class exists, then apply `apps/platform/infisical/manifests/postgresql.yaml` after provisioning its Secret. Do not apply that manifest over the existing `infisical-postgresql` StatefulSet: its current `local-path` volume template cannot be changed in place. Keep the existing bound PVC and use a planned data migration if its storage class ever needs to change.

```bash
helm repo add cert-manager https://charts.jetstack.io
helm repo add traefik https://traefik.github.io/charts
helm repo add infisical-helm-charts https://dl.cloudsmith.io/public/infisical/helm-charts/helm/charts/
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
go run ./cmd/render-site
SITE_CERT_NAME=$(go run ./cmd/render-site --get secretName)
kubectl create namespace observability --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace platform-tests --dry-run=client -o yaml | kubectl apply -f -
# Retain observability-tests for the existing k6 Helm release record.
helm upgrade --install cert-manager cert-manager/cert-manager -n platform-system --create-namespace --version v1.21.0 -f apps/platform/cert-manager/values.yaml --wait
if test -f .generated/manifests/00-selfsigned-issuer.json; then
  CA_CERT_NAME=$(go run ./cmd/render-site --get caSecretName)
  CA_NAMESPACE=$(go run ./cmd/render-site --get caNamespace)
  kubectl apply -f .generated/manifests/00-selfsigned-issuer.json
  kubectl apply -f .generated/manifests/01-ca-certificate.json
  kubectl -n "$CA_NAMESPACE" wait --for=condition=Ready "certificate/$CA_CERT_NAME" --timeout=180s
  kubectl apply -f .generated/manifests/02-ca-issuer.json
fi
for certificate in .generated/manifests/10-certificate-*.json; do kubectl apply -f "$certificate"; done
for namespace in $(go run ./cmd/render-site --get certificateNamespaces); do
  kubectl -n "$namespace" wait --for=condition=Ready "certificate/$SITE_CERT_NAME" --timeout=180s
done
helm upgrade --install traefik traefik/traefik -n platform-system --version 39.0.7 -f core/ingress/traefik-docker-desktop-values.yaml --wait
kubectl apply -f .generated/manifests/20-default-tlsstore.json -f core/exposure/docker-desktop/traefik-lan.yaml
helm upgrade --install infisical infisical-helm-charts/infisical -n platform-secrets --version 0.4.2 -f apps/platform/infisical/values.yaml -f .generated/infisical-values.json --wait
kubectl apply -f apps/platform/infisical/manifests/https-redirect.yaml
# Infisical Operator: use the approval-gated, checksum-pinned local-chart workflow
# in apps/platform/infisical-operator/README.md; a bare upstream upgrade restores broad TokenRequest grants.
helm upgrade --install loki grafana/loki -n observability --version 7.0.0 -f apps/observability/loki/values.yaml --wait
./scripts/helm-monitoring.sh upgrade --install monitoring prometheus-community/kube-prometheus-stack -n observability --version 87.15.1 -f apps/observability/kube-prometheus-stack/values.yaml --wait
kubectl apply -f .generated/manifests/30-dns-blackbox-config.json
kubectl apply -k apps/observability/host-dns-monitoring
kubectl apply -k apps/observability/grafana/manifests/dashboard
helm upgrade --install tempo grafana/tempo -n observability --version 1.24.4 -f apps/observability/tempo/values.yaml --wait
helm upgrade --install alloy grafana/alloy -n observability --version 1.10.1 -f apps/observability/alloy/values.yaml --wait
helm upgrade --install grafana ./apps/observability/grafana -n observability -f apps/observability/grafana/values.homelab.yaml -f .generated/grafana-values.json --set-string infisical.projectSlug="${INFISICAL_PROJECT_SLUG}" --set-string infisical.envSlug="${INFISICAL_ENV_SLUG}" --wait
# Existing k6 release requires its namespace post-renderer; see migrations/k6-platform-tests/README.md.
kubectl apply -k apps/observability/k6/dashboard
```

After installation, confirm `observability/grafana-admin` reports `ReadyToSyncSecrets=True` and the Grafana Deployment is Ready. The image renderer is part of the Grafana chart. For changes to a live release, render and server-side dry-run before applying.

The homelab profiles retain the approved shared anonymous Grafana **Editor** role
and three-day history settings: Loki 72h, Prometheus 3d, Alertmanager 72h, and the
existing Tempo 72h. Anonymous Editor includes dashboard/folder deletion and
alert/notification editing on the existing ingress; it does not grant server or
user administration. Review this profile's access before using it for another
installation. Grafana annotation expiry remains excluded.

Use `scripts/helm-monitoring.sh` for both `template` and `upgrade`. Its temporary,
process-scoped Helm 4 plugin applies the checked-in renderer to the pinned
87.15.1 chart, aligning API-server availability records and dashboard labels to
3d while preserving every alert definition. Bare monitoring Helm commands
restore the chart's 30d records and dashboard and are not the supported path.
Missing, duplicate, changed or previously transformed renderer targets fail
closed. Existing-release upgrades also need the reviewed admission-hook scope;
the completed retention upgrade used `--no-hooks` to leave seven unchanged
hooks unexecuted. First installation still needs its normal admission setup.

The Loki profile now explicitly maps local ruler storage to the sidecar's shared
`/rules` directory and retains the approved sidecar digest. This source correction
has not been deployed or tested with a live rule fixture; review the bounded
Loki rollout/test before changing the existing release.

The Host and DNS dashboard covers the Docker Desktop Kubernetes node, the macOS host, and Technitium DNS. macOS node_exporter must be reachable from the cluster at `host.docker.internal:9101`; this installation already runs it as a LaunchAgent. `host-dns-monitoring/` scrapes that endpoint and probes the generated site hostname through Technitium and Docker Desktop's host DNS endpoint, plus public recursive DNS through Technitium, every 30 seconds. The DNS probe uses a rendered ConfigMap and automatically reloads it, so a site-domain change only needs the generated ConfigMap reapplied. The Technitium HTTP health endpoint can return HTTP 200 with an `invalid-token` payload and does not prove DNS works.

The DNS reconciler is a one-shot macOS LaunchAgent that repeats every 30 seconds. Install it from the repository root with the intended site zone and cluster context. When a VPN owns the default route, set `KUCHDESK_HOST_AGENT_INTERFACE` to the physical LAN interface (currently `en0` on this Mac); select the appropriate interface on another host.

```bash
KUCHDESK_HOST_AGENT_ZONE=$(cd lab && go run ./cmd/render-site --get domain) \
KUCHDESK_HOST_AGENT_KUBE_CONTEXT=docker-desktop \
KUCHDESK_HOST_AGENT_INTERFACE=en0 \
./host-agent/install-macos.sh
```

The k6 Operator uses Prometheus's enabled remote-write receiver. The dashboard ConfigMaps are discovered by Grafana's sidecar. The one-iteration smoke TestRun can be started with `kubectl apply -f apps/observability/k6/tests/smoke-test.yaml`.

## Verification

```bash
kubectl -n observability get pods,pvc
kubectl -n observability get infisicalsecret grafana-admin
kubectl -n platform-tests get deploy,servicemonitor,testruns.k6.io
curl --fail "$(go run ./cmd/render-site --get grafanaUrl)/api/health"
kubectl get --raw '/api/v1/namespaces/observability/services/http:loki:3100/proxy/ready'
kubectl get --raw '/api/v1/namespaces/observability/services/http:tempo:3200/proxy/ready'
kubectl get --raw '/api/v1/namespaces/observability/services/http:monitoring-kube-prometheus-prometheus:9090/proxy/-/ready'
kubectl -n observability get scrapeconfig macos-host
kubectl -n observability get probe technitium-site-dns technitium-recursive-dns docker-host-site-dns
kubectl -n observability rollout status deployment/dns-blackbox
SITE_HOST=$(go run ./cmd/render-site --get grafanaUrl)
dig @127.0.0.1 "${SITE_HOST#https://}" A
```

## Domain and certificate changes

1. Change `domain` in `site.json`, or render a separate profile with `go run ./cmd/render-site --profile path/to/site.json --output path/to/generated`. For a public domain, select an already configured `ClusterIssuer` with `tls.issuer.type: existing` and its name. A wildcard certificate needs a suitable DNS-01 issuer. For a private CA, its Secret namespace must match cert-manager's configured cluster-resource namespace. The renderer does not create DNS records or ACME credentials.
2. Inspect generated manifests and Helm overlays. Apply the new Certificate resources and wait for each certificate to be `Ready` **before** switching Ingress or the default TLSStore. The domain-derived Secret name lets old and new certificates coexist. For an external issuer, skip the private-CA issuer/CA steps. Apply the TLSStore only after its namespace's Secret exists.
3. Update the Technitium DNS zone and the host agent's `KUCHDESK_HOST_AGENT_ZONE` setting to the new domain, then confirm resolution. Update the existing `platform-secrets/infisical-secrets` `SITE_URL` through the established secret workflow to match generated `infisicalUrl`; never put its other secret values in Git. Upgrade Infisical and Grafana with the generated overlays, then verify HTTPS, redirects, and login. Keep old DNS and certificates during a planned transition; remove them separately after verification.

cert-manager renews issued certificates and updates their Secrets automatically. Traefik can reload updated TLS Secrets without a process restart. A domain change to Grafana's `root_url` and Infisical's `SITE_URL` still requires their application pods to reload configuration; with the current single Grafana replica and `ReadWriteOnce` PVC, expect a brief interruption. DNS changes also depend on resolver caches. This is deploy-time reconfiguration and automatic certificate renewal, not hot reload of every application setting.
