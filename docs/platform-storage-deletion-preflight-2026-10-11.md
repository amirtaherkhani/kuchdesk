# Storage deletion preflight — 2026-10-11

**Ten exact candidates verified. Nothing deleted. Final action-time confirmation remains required.**

Fresh API verification: **2026-10-11 05:51:57 UTC**, context `docker-desktop`, node `desktop-control-plane` (containerd 2.3.1). Earlier API/Docker unavailability is resolved; the evidence below supersedes the blocked snapshot. Recheck identities and consumers immediately before presenting the final destructive confirmation.

## Exact candidate set

Every claim is in **`platform-storage`**. All ten are Bound, storage class `standard`, reclaim policy **`Delete`**, volume mode `Filesystem`, access mode `ReadWriteOnce`, hostPath type `DirectoryOrCreate`, node affinity `kubernetes.io/hostname In [desktop-control-plane]`. Each PV claimRef namespace/name/UID matches its PVC. Claims have no owner references and carry `kubernetes.io/pvc-protection` finalizers.

| PVC | Capacity | PVC UID | PV name | PV UID |
|---|---|---|---|---|
| `data-kafka-0` | 8Gi | `62bbc10e-4a81-4d0f-b4ab-4fa676e809eb` | `pvc-62bbc10e-4a81-4d0f-b4ab-4fa676e809eb` | `4faec815-08c5-4b40-b28b-8d48124440ea` |
| `data-minio-0` | 8Gi | `8b05ff69-1dbc-408c-8f7f-00f24ff662d3` | `pvc-8b05ff69-1dbc-408c-8f7f-00f24ff662d3` | `f451a862-7d3c-4b5f-b887-2da06efeeae9` |
| `data-mongodb-0` | 8Gi | `ea394dcc-63da-49e5-b77f-9672a39d4b69` | `pvc-ea394dcc-63da-49e5-b77f-9672a39d4b69` | `eefdd879-bc43-4793-b2ee-b95f4533be41` |
| `data-nats-0` | 4Gi | `459fb6c4-4963-4921-aa68-54e91984b64e` | `pvc-459fb6c4-4963-4921-aa68-54e91984b64e` | `82543624-235a-4e6a-a9de-62d0fc5d6ac6` |
| `data-nats-1` | 4Gi | `89a6f069-2565-4f9f-adf8-22ca98893e47` | `pvc-89a6f069-2565-4f9f-adf8-22ca98893e47` | `b316d466-bea3-40a6-a554-358317f95416` |
| `data-nats-2` | 4Gi | `580a303c-bac6-4e80-abcb-de56d4314f69` | `pvc-580a303c-bac6-4e80-abcb-de56d4314f69` | `658d7e4b-97fc-43dc-84d9-17c406a83ec6` |
| `data-object-storage-0` | 8Gi | `58233832-15e8-455b-a5ab-85176b7631e6` | `pvc-58233832-15e8-455b-a5ab-85176b7631e6` | `3236bec6-2b3e-4e82-bc75-9d94d780b0c2` |
| `data-postgresql-0` | 8Gi | `22d73d43-d3a8-422a-835a-0e1dc67b9a0d` | `pvc-22d73d43-d3a8-422a-835a-0e1dc67b9a0d` | `00b513ab-6cde-4ea0-9a77-13a3c5701ee9` |
| `data-rabbitmq-0` | 4Gi | `3395270b-1731-4e21-95d6-2b1659ca7571` | `pvc-3395270b-1731-4e21-95d6-2b1659ca7571` | `ca555c63-3e5b-4daf-8cf5-3fd3ee6a59a4` |
| `data-redis-0` | 4Gi | `0a29477b-c9d7-4f39-a338-edc7c889b8ae` | `pvc-0a29477b-c9d7-4f39-a338-edc7c889b8ae` | `44ec38ee-9a7e-40ed-8cb0-88eafa9993db` |

**Total: 60 GiB provisioned capacity**, not measured data size. These names match the previous audit. Eight other PVs were inspected solely for path overlap; none has an equal, ancestor or descendant hostPath. They are excluded from deletion.

## Exact node-local backing paths

These paths are inside the Kubernetes node filesystem. They are not established macOS host paths; the underlying Docker Desktop VM/containerd backing location was not mapped.

- `data-kafka-0`: `/var/local-path-provisioner/pvc-62bbc10e-4a81-4d0f-b4ab-4fa676e809eb_platform-storage_data-kafka-0`
- `data-minio-0`: `/var/local-path-provisioner/pvc-8b05ff69-1dbc-408c-8f7f-00f24ff662d3_platform-storage_data-minio-0`
- `data-mongodb-0`: `/var/local-path-provisioner/pvc-ea394dcc-63da-49e5-b77f-9672a39d4b69_platform-storage_data-mongodb-0`
- `data-nats-0`: `/var/local-path-provisioner/pvc-459fb6c4-4963-4921-aa68-54e91984b64e_platform-storage_data-nats-0`
- `data-nats-1`: `/var/local-path-provisioner/pvc-89a6f069-2565-4f9f-adf8-22ca98893e47_platform-storage_data-nats-1`
- `data-nats-2`: `/var/local-path-provisioner/pvc-580a303c-bac6-4e80-abcb-de56d4314f69_platform-storage_data-nats-2`
- `data-object-storage-0`: `/var/local-path-provisioner/pvc-58233832-15e8-455b-a5ab-85176b7631e6_platform-storage_data-object-storage-0`
- `data-postgresql-0`: `/var/local-path-provisioner/pvc-22d73d43-d3a8-422a-835a-0e1dc67b9a0d_platform-storage_data-postgresql-0`
- `data-rabbitmq-0`: `/var/local-path-provisioner/pvc-3395270b-1731-4e21-95d6-2b1659ca7571_platform-storage_data-rabbitmq-0`
- `data-redis-0`: `/var/local-path-provisioner/pvc-0a29477b-c9d7-4f39-a338-edc7c889b8ae_platform-storage_data-redis-0`

## Consumer findings

- **No direct PVC consumers found** across all namespaces in 84 objects: 33 Pods, 17 Deployments, 23 ReplicaSets, 7 StatefulSets and 4 DaemonSets. The query also included ReplicationControllers, Jobs and CronJobs; none were returned. Six zero-replica ReplicaSets were included. Volumes and StatefulSet claim templates were inspected, with no matching retained claim or dedicated target hostPath. No workload in these kinds was present in `platform-storage`.
- **One broad path overlap exists:** `observability/monitoring-prometheus-node-exporter` DaemonSet and Pod `monitoring-prometheus-node-exporter-nbr59` mount hostPath `/` at `/host/root`, read-only with `HostToContainer` propagation. This includes every candidate backing path. It is a monitoring filesystem exposure, not evidence of a running database consumer. It prevents an unconditional claim that no container can see these directories.
- Installed CRDs were inventoried. Storage-capable Prometheus, PrometheusAgent, Alertmanager, ThanosRuler, k6 TestRun and PrivateLoadZone resources were checked across namespaces. Only one Prometheus and one Alertmanager existed; neither configured storage or matching volume references. This is not a proof about every arbitrary custom controller's future behavior.
- No VolumeAttachments exist. HostPath volumes do not require them, so that absence alone proves nothing about use.
- All-namespace Helm release listing found no release in `platform-storage`. No Helm stored Secret contents were read. Repository searches found these claim names in audit/planning documents, with no active references in the searched `docs`, `lab`, `charts` and Host configuration paths. Other repositories, generated manifests and future reconciliations remain outside this evidence.

## Runtime and manual-consumer limits

Docker context was `desktop-linux`. `docker ps -a` returned no visible containers, while `docker info` reported five running containers. Therefore Docker CLI enumeration does **not** establish absence of internal Kubernetes/kind/Docker Desktop mounts. No speculative container was started to inspect them.

A read-only attempt to inspect mount-table metadata through the existing node-exporter mount could not run: its image has no `cat` executable. No new mount, helper Pod, image install or privileged debug container was created. Node process open-file tables, internal runtime mounts, stopped/hidden containers and the macOS-to-VM backing mapping remain unverified. No database files were opened. Offline/manual/external consumers cannot be ruled out.

The earlier [audit](platform-storage-audit-2026-10-10.md) is historical supporting context; fresh identities and observations are recorded above. No usable backup or successful restoration was established, and no backup was created.

## Permanent consequence and final confirmation

The current `standard` StorageClass uses `rancher.io/local-path`. Its live `local-path-storage/local-path-config` maps storage beneath `/var/local-path-provisioner`; the configured teardown runs `rm -rf "$VOL_DIR"`. This script was read, **not executed**. Combined with the verified `Delete` reclaim policies, deleting a confirmed PVC is expected to delete its PV and recursively remove its exact backing directory. Treat all data there as permanently lost; Kubernetes provides no undo. Finalizers or provisioner errors can delay deletion or leave remnants, but are not a recovery mechanism. Do not promise 60 GiB will actually be freed or that every physical byte will be erased.

The user's statement that the data is unimportant establishes intent; **the final action-time confirmation is still outstanding**. Immediately before acting, recheck these exact names, PVC/PV UIDs, bindings, backing paths, policies and consumers. Stop on any mismatch. Present the exact ten-claim set and permanent data-loss consequence, including the node-exporter overlap and manual-consumer limits, and request confirmation to delete only those ten claims. Use UID preconditions for the confirmed objects; do not substitute wildcard or namespace-wide deletion.

**Excluded:** both registry claims, Technitium, Infisical, observability storage, every other PVC/PV, and the `platform-storage` namespace itself. No manual backing-directory cleanup, PV-force deletion, finalizer removal or reclaim-policy changes are included. No deletion authorization is inferred from this document.


## Later read-only node metadata check — 2026-10-11

The existing `desktop-control-plane` Docker container became directly inspectable. No helper container or mount was created. Read-only Docker inspect and node `/proc` metadata improve the earlier runtime limits:

- Docker container ID: `ba5c6a0bc09a679b59240d349082650c379abeab7813cd27e6928031550ba337`; image `kindest/node:v1.36.1`.
- Node `/var` is backed by Docker volume `da474d088f34ca191cc5e341cdc2950321bec1376a66eb7b5ea13cc8b8e10b31`. Docker reports VM-side source `/var/lib/docker/volumes/da474d088f34ca191cc5e341cdc2950321bec1376a66eb7b5ea13cc8b8e10b31/_data`. Node mountinfo shows the corresponding ext4 `/dev/vda1` mount rooted at `/docker/volumes/da474d088f34ca191cc5e341cdc2950321bec1376a66eb7b5ea13cc8b8e10b31/_data`. These are Docker Desktop VM paths, not a verified macOS directory. The volume also contains runtime and other storage: **deleting this volume is excluded**.
- Scanning readable `/proc/<pid>/mountinfo` in the node found no explicit mount entry containing a candidate `local-path-provisioner/..._platform-storage_...` directory. Parent mounts such as `/var` and node-exporter's root exposure still cover those directories.
- Read-only `fd`, `cwd` and `root` symlink inspection checked 3,961 process links, with one unavailable/raced link and zero links directly into the candidate backing directories. No process arguments, environment variables, credentials or data-file contents were read.

This is a point-in-time metadata observation, not proof that no consumer exists: process races, unreadable entries, alternate bind paths, Docker Desktop processes outside the node PID namespace, offline consumers and future controllers remain limits. The exact ten-PVC action-time confirmation boundary and all exclusions above are unchanged. No storage mutation occurred.
