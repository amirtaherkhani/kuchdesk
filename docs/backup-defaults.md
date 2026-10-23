# Operational backup defaults

Use native local database dumps/exports without an additional encrypted container by default. Do not automatically introduce a passphrase, encrypted sparsebundle or Keychain requirement. This does not change encryption already used internally by a database, nor decrypt or delete existing backups.

- Write backups outside Git into an explicitly selected private local directory, with directory mode `0700` and file mode `0600`. Verify ownership and permissions; keep values out of logs, chat and committed artifacts.
- Record engine/version, source identity, export format, timestamp, checksums and scope without storing credentials in the manifest. Preserve the engine-specific roles/schema/data needed for a valid restoration within the agreed scope.
- Test restoration into an isolated disposable destination before calling a backup restorable. Record the checks and outcome; never rehearse restoration over a live database.
- Account for credentials/keys and related state required by the application through its existing private workflow, without silently copying unrelated private session state.
- Retain existing backup artifacts until a separate retention decision. A new native export does not authorize altering old encrypted backups.

These are operational defaults, not a request to start a backup. The completed k6 namespace move involved no database dump or PVC operation.
