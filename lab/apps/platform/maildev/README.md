# MailDev development sink

The `maildev` release in namespace `vero` owns Deployment and Service
`finance-maildev`. Its live ClusterIP listens on SMTP1025 and HTTP1080. It has no
outgoing relay, SMTP credentials, PVC,
or mounted mail directory. Captured messages are disposable and are lost when
the Pod is replaced. Network policy allows development SMTP on TCP 1025 from Mac, bridge-container,
and Pod sources and declares deny-all outbound traffic. A separate UI/API
`kube-system/kuchdesk-ui` release permits only Traefik to reach HTTP1080 and
provides `https://maildev.local.dev` with the approved Mac source filter and
panel login. Ordinary Pods remain denied raw1080; they use the stable authenticated API
Service described below.

The image is the official `maildev/maildev:2.2.1` from
[MailDev v2.2.1](https://github.com/maildev/maildev/tree/v2.2.1), pinned to
manifest-list digest `sha256:180ef51f65eefebb0e7122d8308813c1fd7bff164bc440ce5a3c2feee167a810`.
The verified Linux/arm64 manifest digest is
`sha256:9bf45940731ffbb9a0c82da5de56a8543e1e4f96d688e1ae1cdef663c3bf5aad`.
The upstream image exposes 1025 and 1080; this chart sets those listeners
explicitly. The upstream `--mail-directory` option supports persistence, but
this disposable development installation intentionally does not set it.

All cluster Pods use `finance-maildev.vero.svc.cluster.local:1025`. The canonical
catalog allocates SMTP publication on VM/Mac TCP 20001 and API on TCP 20002.
The Helm-owned `finance-maildev-smtp` LoadBalancer publishes TCP 20001 on the
Colima VM, and Lima currently forwards it to Mac `127.0.0.1:20001`. Mac and
ordinary Pod and bridge-container synthetic SMTP captures passed in the
2026-10-19 infrastructure acceptance. Unauthenticated raw API publication remains
disabled; the shared authenticated API and protected HTTPS panel are available. See the
[local service access guide](../../../../docs/local-service-access.md) before
applying or testing publication. The `vero/maildev` release remains the sole
owner; Finance's dependency chart no longer defines MailDev resources.

## Shared authenticated API (2026-10-19)

Mac SMTP/API use127.0.0.1:20001 and http://127.0.0.1:20002. Ordinary bridge
containers use the verified profile VM192.168.64.15 with those ports. All Pods
use SMTPfinance-maildev.vero.svc.cluster.local:1025 and authenticated HTTP
kuchdesk-maildev-api.kube-system.svc.cluster.local:20002. The dedicated API
Service is owned by kube-system/kuchdesk-ui and targets Traefik, retaining the
existing panel BasicAuth. Raw1080 is still Traefik-only; no Finance-only source
restriction or ephemeral Pod-IP allowlist applies. Outgoing relay remains off.
MailDev publications use explicit local source ranges and no NodePorts.
See the canonical access guide for the verified handoff and transport boundary.
