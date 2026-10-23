# KuchDesk

Go tools for Kubernetes discovery, scoped management, macOS DNS reconciliation, diagnostics, deployment, and local observability. The cluster agent exposes authenticated HTTP/SSE APIs. Host tools provide a one-shot Technitium DNS job, Doctor checks, an optional HTTPS Doctor bridge, and opt-in image/Helm deployment.

## Components

- [`kuchdesk-agent/`](kuchdesk-agent/README.md): Kubernetes discovery, metrics, SSE, CoreDNS alias support, and explicitly enabled management.
- [`host-agent/`](host-agent/README.md): macOS DNS reconciliation, Doctor, MCP, and deployment workflow.
- [`configyaml/`](configyaml/values.go): shared canonical YAML access with private-value redaction and scoped edits.
- [`lab/`](lab/README.md): directly consumed Helm values and the local DNS, Traefik, TLS, Grafana, Prometheus, Loki, Tempo, Alloy, and k6 profile.

The dashboard is not included. The checked-in fresh-install profile targets Colima with K3s; the previous Docker Desktop installation is not an input.

## Local configuration

Each active chart has tracked direct values YAML under its chart or `lab/apps/` directory. Grafana, Technitium, and the Kubernetes Agent also consume Git-ignored `values.local.yaml` files for new credentials. Their `values.local.example.yaml` files contain placeholders. Keep private files mode 0600, pass both values files explicitly with `helm -f`, and never commit or print real values.

The site renderer reads [`lab/site.json`](lab/site.json) for DNS probes and new certificate resources. Grafana's hostname and TLS Secret name are configured directly in its values YAML and must match the site profile. K3s supplies CoreDNS and Traefik; the project configures Traefik through direct YAML. Technitium has its own new chart and claim. These inputs do not use credentials, volumes, or addresses from the old cluster.

Use the [local service access guide](docs/local-service-access.md) for macOS,
ordinary-container, Pod, and HTTPS panel endpoints and their verification.

## Verify locally

```sh
go -C configyaml test ./...
go -C kuchdesk-agent test ./...
go -C host-agent test ./...
go -C lab test ./...
```

Rendering, tests, and builds do not deploy services. See the [lab guide](lab/README.md) for the reviewed installation commands and the [deployment checklist](docs/deployment-completion.md) for live rollout checks.
