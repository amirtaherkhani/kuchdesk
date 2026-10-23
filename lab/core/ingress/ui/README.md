# Local UI HTTPS routes

This chart adds protected routes to existing Services. It owns no application
workload, PVC, certificate or credential Secret. It also owns the dedicated
authenticated MailDev API publication Service, separate from the HTTPS panel. Install it as
`kube-system/kuchdesk-ui`; preserve each backend's original Helm owner.

Endpoint names, namespaces, domains and ports come directly from
[`lab/service-access.yaml`](../../../service-access.yaml). `values.yaml` contains
the approved source filter and login policy. Grafana keeps its existing Ingress.
All new routes use the existing default wildcard TLSStore on `websecure`.

```sh
python3 lab/scripts/helm-service.py ui --context colima-kuchdesk-fresh
python3 lab/scripts/helm-service.py ui --context colima-kuchdesk-fresh \
  --apply --confirm ui/colima-kuchdesk-fresh
```

The helper passes both canonical YAML files to Helm. It never passes private
credentials in values or generates accounts. The approved panel login is stored
in ignored mode0600 `auth.local.yaml`; only its bcrypt htpasswd record is installed
as Secret `kuchdesk-ui-basic-auth`, key `users`, in vero, observability and
kube-system. RabbitMQ and Technitium use native login. The RabbitMQ monitoring
identity is stored in its existing private platform auth YAML.

Before enabling a new route, verify the Mac source address at Traefik. The
approved Service setting is `externalTrafficPolicy: Local`; a masqueraded node
address is not an acceptable replacement for the source filter. A missing auth
Secret/middleware must leave the route unusable. New endpoints default to the
panel login unless explicitly listed under `nativeAuth`.

Companion policies allow only kube-system Traefik Pods to reach MailDev1080 and
RabbitMQ15672. SMTP and AMQP rules and egress denial remain owned by their backend
charts. The API entrypoint20002 has its own local-source LoadBalancer Service without
NodePorts. Existing BasicAuth protects all client modes; raw backend1080 remains
Traefik-only. No ephemeral Pod-IP allowlist or outgoing relay is used.

See the [canonical access guide](../../../../docs/local-service-access.md) for
verified URLs, credential locations and the current access boundary. Chart
rendering does not establish authentication, TLS trust or application health.
