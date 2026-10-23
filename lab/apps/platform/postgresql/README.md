# Shared postgresql

Copied from Finance deploy/helm/finance-dependencies server definitions. Internal Service `postgresql.platform-storage.svc.cluster.local:5432`, publication TCP20003. Separate Helm release `postgresql` in platform-storage and fresh kept PVC; no Finance volume/data reuse. Image publisher version/index and arm64 digest are recorded in the task diagnostics. No native web UI. Secret `postgresql-auth` is supplied separately from ignored auth.local.yaml; Helm receives references only.

Initialization, resources, probes, container/server ENV and mounted authentication match Finance. Resource names, namespace, local-path storage selection, shared authenticated protocol ingress and a companion publication Service differ for the approved shared architecture. Deployment/credential acceptance is pending. See [canonical access](../../../../docs/local-service-access.md).
