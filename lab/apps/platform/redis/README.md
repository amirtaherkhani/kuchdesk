# Shared redis

Copied from Finance deploy/helm/finance-dependencies server definitions. Internal Service `redis.platform-storage.svc.cluster.local:6379`, publication TCP20004. Separate Helm release `redis` in platform-storage and fresh kept PVC; no Finance volume/data reuse. Image publisher version/index and arm64 digest are recorded in the task diagnostics. No native web UI. Secret `redis-auth` is supplied separately from ignored auth.local.yaml; Helm receives references only.

Initialization, resources, probes, container/server ENV and mounted authentication match Finance. Resource names, namespace, local-path storage selection, shared authenticated protocol ingress and a companion publication Service differ for the approved shared architecture. Deployment/credential acceptance is pending. See [canonical access](../../../../docs/local-service-access.md).
