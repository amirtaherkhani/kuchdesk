# Shared local RabbitMQ

Single-node local development broker, official Finance test-contract image
4.2-management-alpine pinned by verified index digest. The
platform-storage/rabbitmq release retains its4Gi local-path PVC, internal
Service5672 and high publication20005. Only Traefik may reach management15672;
the separate kube-system/kuchdesk-ui release provides
`https://rabbitmq.local.dev` with the approved Mac source filter.

The existing Finance user/password and permissions are unchanged. A separate
`kuchdesk-ui` identity has the monitoring tag and no application-resource
configure/write/read permissions. Its password is different from the shared
panel login. Definitions import creates scoped users/vhosts without a default
guest user. Authentication and cookie come from ignored mode0600 auth.local.yaml
into rabbitmq-auth; UI metadata is under uiClient. No broker restart was needed.

`.helmignore` now excludes auth.local.yaml from future packages. An inspection
found that the pre-existing installed revision2 had embedded that private file
and the existing application password. That historical copy was retained;
history deletion or credential rotation is outside this UI task. The newly
created UI password is absent from that history. Do not claim that old Helm
release records are credential-free. Additional projects require their own
vhost/user configuration. See the
[access guide](../../../../docs/local-service-access.md).
