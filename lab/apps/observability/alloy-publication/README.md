# Alloy OTLP publication

This Helm-owned companion Service publishes only the Alloy OTLP receivers.
The internal `alloy` ClusterIP Service remains the Pod endpoint. Source ranges
cover the configured Colima/Lima interfaces, the containerd bridge and the
single-node k3s Pod CIDR so Mac, ordinary-container and Pod clients can
reach these development receivers. The Mac Lima listeners still bind only to
`127.0.0.1`; the VM-side published Service is a separate network path. The
Alloy UI is not included. Canonical port assignments are in
`lab/service-access.yaml`.
