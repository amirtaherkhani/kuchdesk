# Finance NATS access in Development

The local `FINANCE` account has two identities. The API user retains its narrow
canonical event/DLQ publish, consumer and acknowledgement permissions. A separate
NKey operator can manage JetStream streams, messages and consumers in this account
and publish/subscribe under `vv.finance.>`. It has no system-account or unrelated
subject access. There are no account imports or exports.

The operator can delete canonical Finance data. Use it only for explicit
Development administration and resource-owned diagnostics; never inject its seed
into Finance API pods or reuse it for the application identity. Production
permissions are outside this local configuration.

Keep the public key in `platform-storage/nats-finance-operator` (`public-key`),
and the private seed in `vero/finance-nats-operator` (`seed`, `public-key`). Preserve
their ignored, mode-0600 native manifests in `auth.local.yaml` alongside the existing
NATS authentication manifest. Never print the seed. Only an explicitly authorized
diagnostic Job mounts the operator Secret; the broker and API pods never receive
the private seed. The Job uses the pinned main Finance image and namespace `vero`,
with a separate component label so the API Service cannot select it.

Apply the native Secret manifest before the pinned official `nats-2.15.1` chart
with `values.yaml`. Adding the public-key environment reference requires one
StatefulSet Pod replacement. Preserve the NATS PVC, existing API credential,
Service names, images and chart version. Later ACL-only changes can reload via
the existing config reloader, subject to rendered-diff verification.

For diagnostics, pre-generate unique stream/consumer identities and subjects;
verify absence before creation; tag streams with an ownership nonce; and cap
messages, storage, concurrency and duration. Reconcile both attempted stream
names after success or failure, including a lost create reply. Delete only a
matching ownership nonce and exact subject set, then verify each name returns
not found. Record canonical config/state before and after. Never purge a shared
stream to clean a probe.
