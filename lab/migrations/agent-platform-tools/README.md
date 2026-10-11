# KuchDesk agent namespace stage

The approved move completed on 2026-10-10: Deployment, Service and ServiceAccount `kuchdesk-agent` now live in `platform-tools`. Helm release `kuchdesk-agent` remains in `development-tools`, chart **0.13.0**, revision **7**. The target became Ready after **6.4 seconds**. Existing source Secrets and the public CA ConfigMap were retained.

Fresh 2026-10-11 checks confirm revision 7 deployed and target 1/1 Ready. The stored revision-6 manifest transformed by `post-renderer.py` equals the current Helm manifest exactly: three resource namespace fields and two ServiceAccount subject namespaces change. Image, environment, CoreDNS Role and permission rules remain identical. Both source/target Secrets and the public CA data match; the three source workload objects are absent.

The internal endpoint is `http://kuchdesk-agent.platform-tools.svc:8080`. For a trusted local client, forward `kubectl -n platform-tools port-forward --address 127.0.0.1 svc/kuchdesk-agent 8080:8080`; supply the existing bearer through a private client process, never command output or Git. Helm's chart NOTES still derive DNS from the release namespace and are not the migrated endpoint.

## Future Helm changes

Do not run an ordinary unrendered upgrade: it would move workloads back to the release namespace. The generic Host deployment workflow also lacks post-renderer support and waits in its release namespace; it cannot currently upgrade this migrated release correctly.

This stage used the chart at commit `a0619b7`, which exactly reproduced revision 6 with its then-current values. The latest local chart already added three project-metadata environment variables, so it was not used for the namespace-only move. For future upgrades, review the complete pinned input/output diff and choose the actual pre-change revision for rollback. Preserve existing values and Secret references; never put credential values in Helm inputs/history.

The renderer requires Python 3 and Mike Farah `yq`. For Helm 4, create a process-scoped postrenderer/v1 plugin descriptor:

```yaml
apiVersion: v1
type: postrenderer/v1
name: agent-move
version: 0.1.0
runtime: subprocess
runtimeConfig:
  platformCommand:
    - command: /absolute/repository/lab/migrations/agent-platform-tools/post-renderer.py
```

Place it at `<private-plugin-root>/agent-move/plugin.yaml` and use `HELM_PLUGINS=<private-plugin-root>` with `--post-renderer agent-move`, release `kuchdesk-agent` and `--namespace development-tools`. Set the script path to the maintained checkout before retiring any worktree. This plugin is needed for Helm rendering only; the running agent does not use it.

The reviewed scope is exactly six chart objects. The renderer rejects missing/extra/duplicate resources, unexpected namespaces, SA subjects or role references. It leaves the CoreDNS Role in `kube-system`, rewrites only the approved RoleBinding and ClusterRoleBinding SA namespace, and never creates/copies Secrets.

Run `python3 lab/migrations/agent-platform-tools/test_renderer.py` for the full-field preservation contract and rejection checks against a rendered current chart. New resources or privileges require a fresh scope review; passing the renderer alone does not approve arbitrary future changes.

## Health and rollback

The stage's readiness gate was five minutes, with rollback to revision **6** in `development-tools`; rollback was not required. Source Secret copies and source namespace remain available. The user explicitly accepted that any in-flight exec/tunnel/queued operation could be interrupted because the agent has no complete idle/drain inventory.

Fresh API checks pass `/healthz`, `/readyz`, authenticated snapshot, unauthenticated 401, SSE connect/reconnect, metrics API and in-cluster DNS/ClusterIP health access. After restoring Docker on 2026-10-11, initial Host checks returned an enrollment warning/`project_denied`; the existing controller recovered as startup settled. Final fresh metadata reads returned both projects, and Doctor passed 8/8 with enrollment `ready`, without login or grant changes. See the execution checkpoint in [migration plan](../../../docs/namespace-migration-plan.md).

Complete the [deployment/image checks](../../../docs/deployment-completion.md) only after all required integration gates pass. No image cleanup is included in this namespace stage.
