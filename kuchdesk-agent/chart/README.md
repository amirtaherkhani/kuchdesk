# KuchDesk Agent Helm chart

The chart installs the Kubernetes discovery and management agent. It defaults to read-only discovery; CoreDNS writes, general management, exec, port forwarding, and the HTTPS Host Bridge are separate opt-ins. Set `cluster.id`, `cluster.name`, an immutable `image.tag`, and `image.repository` explicitly. Tags must use `git-<12-hex>` or `git-<12-hex>-dirty-<12-hex-input-hash>` from the actual source/build inputs. Set `image.digest` to the verified digest when available in the target store.

## Canonical Helm values

Use tracked `values.yaml` plus ignored `values.local.yaml` for a new local deployment. Copy the key names from `values.local.example.yaml`, generate a fresh `auth.token`, set `auth.existingSecret: ""`, and keep the private file mode 0600. Helm supplies Pod ENV and creates the Agent bearer Secret; the Pod references that same Secret. Mac files are never mounted into the Pod. Every Agent API endpoint requires bearer authentication; health/readiness probes remain unauthenticated. Existing installations can continue to use `auth.existingSecret` without placing its value in Helm input. Do not set both.

```sh
helm lint ./kuchdesk-agent/chart -f ./kuchdesk-agent/chart/values.local.yaml \
  --set cluster.id=example --set cluster.name='Example cluster' \
  --set image.repository=example.invalid/kuchdesk-agent \
  --set image.tag=git-123456789abc
```

Rendering Helm output can expose Secret values; redirect it to a private file when review is necessary. Changes to the local values file do not rotate credentials in an existing running installation without an explicit upgrade and any required client updates.

## Access boundaries

- `dnsManagement.enabled` permits edits only to the configured existing CoreDNS ConfigMap when its narrow Role is installed. Back up and review the Corefile before a live change.
- `agent.managementEnabled` requires an agent bearer and `rbac.clusterAdmin=true`. This grants the dedicated Agent ServiceAccount broad Kubernetes API access and must be approved for the target cluster.
- `agent.execEnabled` and `agent.portForwardEnabled` require management. Port forwarding is short-lived and uses a loopback listener inside the Agent Pod.
- `hostBridge.enabled` requires an HTTPS origin, a separate bridge bearer (`hostBridge.token` in private local YAML or `tokenSecretName`), and optionally `caPEM` or `caConfigMapName`. Inline private values create dedicated Secret/ConfigMap resources; use one source per resource. Supply the issuing CA certificate, never the server private key or self-signed leaf. The deployment checksums that public CA input so a trust-root update rolls the Agent. The bridge forwards only authenticated Doctor requests and works with read-only discovery; it does not enable Kubernetes management or expand RBAC. It has no public Service route by default.

The Agent still runs when the Metrics API is unavailable; CPU and memory measurements are then absent. `service.targetPort` sets the HTTP listener port. A chart upgrade and Pod rollout are required for changed environment values. See [the Agent API](../README.md) and [the Host Bridge boundary](../../docs/host-control-plane.md).

For Colima containerd/k3s, build and deliver the real Agent image into the
selected profile's Kubernetes-visible `k8s.io` namespace. Verify image content,
platform, tag and digest there before installation. No direct host files or
host UID/GID mapping are required. The removed `configFiles` setting is rejected
so an old overlay cannot silently retain that architecture. Synthetic image
identities are suitable only for explicit render tests. Host/MCP remains the
configuration read/edit surface; the optional Host Bridge preserves its
separately authenticated Doctor path.
