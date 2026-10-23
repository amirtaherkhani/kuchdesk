# Host helper inventory

- `kuchdesk-host-agent`: one-shot Technitium DNS reconciliation and explicit Grafana password recovery.
- `kuchdesk-doctor`: read-only host and cluster diagnostics.
- `kuchdesk-deploy`: explicit local image build and Helm deployment.
- `kuchdesk-mcp`: unregistered source for stdio Doctor, canonical YAML read/edit and opt-in deployment tools.
- `kuchdesk-host-bridge`: optional HTTPS Doctor endpoint.

Use direct chart values YAML and ignored `values.local.yaml` overlays for new local credentials.
