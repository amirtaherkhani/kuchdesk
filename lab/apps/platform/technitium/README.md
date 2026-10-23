# Technitium for a new K3s cluster

On the current installation, `https://technitium.local.dev` uses the existing
admin login and the route-only UI chart's Mac source filter. Native login and
logout were verified without changing DNS records, the CA, the trust store,
the existing password or the Host Agent's internal access path.

The chart creates a new Technitium StatefulSet, local-path PVC, internal Service, DNS-only LoadBalancer Service, and admin Secret. It does not import the old instance or claim. The first-start password comes from ignored `values.local.yaml` (mode 0600); copy only the key name from `values.local.example.yaml`. The account name is Technitium's fixed `admin`.

For the later authorized deployment, pass both files to Helm with `-f values.yaml -f values.local.yaml`. The Host Agent reads the same private YAML directly, then uses namespace `technitium` and Service `technitium`. The chart creates Secret `technitium-admin`, key `DNS_SERVER_ADMIN_PASSWORD`, for first start. Confirm the DNS LoadBalancer address and macOS route before enabling the Host Agent or changing host DNS.

The upstream image reads `DNS_SERVER_ADMIN_PASSWORD` only on an empty `/etc/dns` configuration. The new PVC and selected K3s context are required for fresh credentials to take effect. Rendering or editing values does not rotate an existing volume.
