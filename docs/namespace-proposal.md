# Namespace organization proposal

> Historical proposal from 2026-10-10. Secret-manager and identity sections below are superseded by the direct YAML configuration. They are retained only as dated migration evidence and must not be used as deployment instructions.

Status: architecture/proposal only. Inventory observed on 2026-10-10; the approved k6-only stage is complete as recorded below. Other migrations, shared database deployment and new credential provisioning remain proposals.

The retained functional groups are `observability`, `platform-secrets`, `platform-tools`, `platform-networking` and `platform-tests`. Use **`platform-databases`** for proposed shared PostgreSQL and future database services, and **`platform-messaging`** for NATS, RabbitMQ, Kafka and similar brokers. Redis belongs with the function it actually serves; its proposed shared-service placement is not yet a deployed fact. Kubedesk Platform owns these seven shared infrastructure groups. Each independent managed project has its own Infisical project and dedicated configurable Kubernetes namespace, separate from the platform groups. No namespace prefix is mandatory; no namespace is created automatically by Infisical discovery.

## Consolidated seven platform groups

| Group / proposed namespace | Current location and observed workloads | Proposed responsibility |
|---|---|---|
| `observability` | `observability`: Grafana/renderer, Prometheus, Alertmanager, Loki, Tempo, Alloy and exporters | Monitoring, logs and traces; retain grouping. |
| `platform-secrets` | `platform-secrets`: Infisical, Operator, Infisical PostgreSQL and Redis | Secret management and its internal dependencies; retain grouping and data. |
| `platform-tools` | `development-tools`: KuchDesk agent; `platform-system`: local registry | Agents and developer infrastructure. |
| `platform-networking` | `technitium`: DNS; `platform-system`: Traefik and cert-manager | DNS, ingress and certificates. |
| **`platform-tests`** | Current workloads run in **`platform-tests`** after the approved k6 stage; Helm release metadata remains in `observability-tests` | k6 and future general testing tools. k6 migration complete; future testing tools remain proposed. |
| **`platform-databases`** | No running shared database workload verified in `platform-storage`; retained PVC inventory below | Shared PostgreSQL and future database services for managed projects, with engine-specific project isolation. Redis used as a data/cache service may belong here. This is a new service design, not permission to adopt old volumes. |
| **`platform-messaging`** | No running NATS, RabbitMQ or Kafka workload or matching Service/current Helm release found in the fresh inventory | Shared broker infrastructure and engine-specific project accounts/ACLs for NATS, RabbitMQ, Kafka and similar systems; proposed only. |

`kube-system`, `kube-public`, `kube-node-lease`, `local-path-storage` and `default` remain unchanged. The existing `platform-storage` namespace and all **10 bound PVCs** remain untouched pending ownership and recovery review.

## Independent project namespace mapping

Keep an explicit mapping keyed by stable Infisical **project ID**, with environment and Kubernetes namespace as configurable fields. Project names and slugs are display/routing metadata, not immutable identity. `finance` and `vero-finance` are distinct strings; do not substitute one for the other or rename resources.

| Infisical project ID | Current display name | Environment | Namespace mapping status |
|---|---|---|---|
| `85cbdbe6-80dd-4e31-aa45-2f0ac2263588` | Kubedesk Platform | `dev` | Owns the shared platform groups and observability; current namespaces remain until an approved migration. |
| `fd27d482-48bc-4b6c-b194-5901bdbc497b` | vero-finance | `dev` | `vero` is a user-supplied example namespace, not a required name. The final namespace is configurable; provisioning is not performed or implied. |

Proposed configuration shape (documentation only, not an implemented provisioning API):

```json
{
  "projects": [
    {
      "infisicalProjectId": "fd27d482-48bc-4b6c-b194-5901bdbc497b",
      "environments": [{"infisicalEnvironment": "dev", "kubernetesNamespace": "vero"}],
      "namespaceProvisioning": "disabled"
    }
  ]
}
```

Validate mapping uniqueness and project/environment existence before use; reject ambiguous or conflicting namespace assignments. Applications run in their own namespace and consume shared database/broker endpoints with project-scoped data, credentials and access. Cross-namespace connectivity and secret delivery require an explicit policy; the existing read-only Infisical identity is not permission to broaden Kubernetes privileges.

Existing automatic Infisical discovery reconciles approved project access only. Namespace creation, workload installation and database/broker provisioning are separate explicitly enabled actions, not side effects of discovery. Preserve current Host Admin/K8 read-only roles, exclusions and deliberate revocations.

## Dashboard placement

Place a dedicated broker UI with its broker in `platform-messaging`, a database UI with its service in `platform-databases`, and generic management web applications in `platform-tools`. Grafana remains in `observability`; its dashboards may visualize all groups without moving the underlying services. No new dashboard installation is implied. Keep Infisical's UI and internal dependencies in `platform-secrets`, and Technitium's service UI with DNS in the planned `platform-networking` group.

## Shared database boundaries

Shared infrastructure does not mean shared application credentials or unrestricted data access. Configure an endpoint, logical database/key namespace, project identity and credential reference for each project and environment. Keep credentials in the established secret workflow, with separate rotation and revocation; provision none as part of this proposal.

| Engine | Proposed project boundary | Required design checks before deployment |
|---|---|---|
| PostgreSQL | Separate logical database per project/environment; distinct application roles and credentials; separate restricted migration/owner roles where needed | Restrict connection, schema and object privileges, including inherited and `PUBLIC` grants. Applications receive neither superuser nor role/database administration privileges. Verify denied cross-project access and backup/restore boundaries. |
| Redis | Separate ACL user and credential per project/environment; restricted key prefixes, Pub/Sub channel patterns and command allowlists | A numeric Redis database selected with `SELECT` is not a security boundary. Avoid unrestricted administrative/global commands. Verify every required client command and cross-project denial. Shared process, memory, persistence and availability remain common; choose a dedicated service when a workload needs stronger isolation. |
| Future engines | Native database, schema, tenant or equivalent boundary plus separate identities | Define engine-specific access, quotas, backup/restore, rotation and cross-project tests before adoption; do not assume PostgreSQL or Redis semantics apply. |

The PostgreSQL proposal follows its [privilege model](https://www.postgresql.org/docs/current/ddl-priv.html). Redis isolation uses [ACL users, command and key restrictions](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/); [Redis's SELECT documentation](https://redis.io/docs/latest/commands/select/) cautions against using logical database numbers for unrelated applications. Logical access isolation does not provide separate resource capacity or failure domains.

Infisical's existing internal PostgreSQL and Redis **remain in `platform-secrets`**. Do not reuse their credentials, move their data, or expose them as the shared application service. Any later consolidation would require its own explicit design, backup/restore verification and migration approval.

## Messaging: current versus proposed

A fresh read-only cluster-wide inventory on 2026-10-10 scanned **91 Pods, StatefulSets, Deployments and Services**, plus the current Helm release listing. Names and container images were checked for NATS, RabbitMQ, Kafka and common related broker names. This is a bounded cluster inventory, not a check of externally hosted services.

| Service | Current namespace | Verified status | Proposed placement |
|---|---|---|---|
| NATS | None running found | No matching Pod, StatefulSet, Deployment, Service or current Helm release | `platform-messaging` if later deployed |
| RabbitMQ | None running found | No matching Pod, StatefulSet, Deployment, Service or current Helm release | `platform-messaging` if later deployed |
| Kafka | None running found | No matching Pod, StatefulSet, Deployment, Service or current Helm release | `platform-messaging` if later deployed |
| Infisical Redis | `platform-secrets` | `redis-master-0` Running and container ready; StatefulSet `redis-master` **1/1 ready**; `redis-master` and `redis-headless` Services exist | Remains an Infisical-internal dependency in `platform-secrets` |
| Future shared Redis | Not deployed/verified | No separate shared Redis workload found | Classify by actual role: data/cache under `platform-databases`; a dedicated messaging role may fit `platform-messaging` after design review |

Retained NATS/RabbitMQ/Kafka PVCs are not proof of running brokers. Old monitoring links are not runtime evidence. Infisical Redis is not treated as shared broker capacity, and its actual internal usage is not inferred solely from the Redis image.

The messaging design should specify per-project broker-native boundaries (accounts, vhosts, topics/subjects or equivalent), separate credentials and ACLs, and engine-specific cross-project denial tests. No broker, account, credential or permission is provisioned by this proposal.

## Existing ownership evidence

Read-only Kubernetes metadata and Helm inventory showed:

- `platform-storage`: no Deployments, StatefulSets, DaemonSets, Pods or Services returned; the current Helm listing returned no releases. This does not establish historical ownership or prove that the retained data is unused.
- All ten PVCs are `Bound`, have no owner references and no `meta.helm.sh/release-name` annotation. Labels provide historical hints only; actual data owners and consumers are **unverified**.
- `platform-secrets/infisical-postgresql` exists with no Helm release annotation. Repository manifests under `lab/apps/platform/infisical/manifests/postgresql.yaml` define the Infisical-specific database. Its Service selector identifies the PostgreSQL/database component; metadata does not establish shared-project ownership.
- `platform-secrets/redis-master` is labeled and annotated as belonging to Helm release `infisical`; its Service selector also targets that release.

| Retained PVC(s) in `platform-storage` | Application label | Instance label |
|---|---|---|
| `data-postgresql-0` | `postgresql` | `platform-storage` |
| `data-redis-0` | `redis` | `platform-storage` |
| `data-mongodb-0` | `mongodb` | `platform-storage` |
| `data-kafka-0` | `kafka` | `platform-storage` |
| `data-minio-0` | `minio` | `platform-storage` |
| `data-nats-0`, `data-nats-1`, `data-nats-2` | `nats` | `platform-storage` |
| `data-rabbitmq-0` | `rabbitmq` | `platform-storage` |
| `data-object-storage-0` | `object-storage` | `object-storage` |

The retained inventory also includes messaging and object-storage data. Those labels do not make every volume a database or assign it to `platform-databases`. No data contents or credentials were inspected.

## Before any migration

Review and approve a concrete migration plan covering PVC ownership, backups and restore checks; Helm release ownership; RBAC and service accounts; service DNS; certificates and network policies; Secret/Operator references; and Infisical authentication bound to namespace and service account. Specify shared-service resource limits, availability and per-project restore expectations.

Keep namespace names, service endpoints, database names and credential references configurable through Helm values and agent configuration. Apply supported reload behavior only where implemented; identity bindings and process settings may require restart. Do not hardcode proposed names into running services before migration approval.

System namespaces and the 10 bound `platform-storage` PVCs remain untouched. This document changes neither cluster configuration, permissions nor credentials.

See [migration preparation](namespace-migration-plan.md) for the resource mapping, approval gates, health checks and rollback sequence.

Base project display metadata defaults to `KUCHDESK_BASE_PROJECT_NAME=Kubedesk Platform`; `KUCHDESK_BASE_PROJECT_ID` and `KUCHDESK_BASE_PROJECT_SLUG` remain separately configurable. Do not derive immutable identity from the display name. See [backup defaults](backup-defaults.md) and [retained storage audit](platform-storage-audit-2026-10-10.md).
