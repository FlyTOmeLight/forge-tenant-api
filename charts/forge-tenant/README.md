# Tenant Helm deployment

`existingSecret` supplies the application environment for standalone installs; `caSecret` supplies the trust bundle at `/ca`. Credentials and private keys remain in Kubernetes Secrets and are not rendered into Helm values. `fullnameOverride` replaces all chart resource names; standalone installs without it retain the release name as their service fullname.

When `global.forge.enabled=true`, the fullname defaults to `<release>-tenant` and the API, worker and their Tenant init containers use `global.forge.tenantImage` (`repository`, `tag`, `pullPolicy`), shared with the `forge-tenant-init` chart. Standalone installs continue to use `image`. The parent chart supplies `<release>-tenant-env`, `<release>-external-credentials`, `<release>-artifact-env`, and `<release>-tls`; tenant mounts only `ca.crt` from the TLS Secret at `/tls`, and tenant certificate/key from that Secret into the API TLS sidecar. The external credential Secret is projected as a whole directory at `/external`, so init containers wait for the `BOOTSTRAP_COMPLETE` marker and required keys before migration and bootstrap. Both API and worker run `/forge-init wait-external` with a fixed 300-second budget before their `/forge` commands. The worker waits on the same marker, then migrates independently; it has no API dependency.

For Artifact integration, configure:

- `worker.existingSecret`: additional worker-only environment (the deployed integration uses `STORAGE_URL` and `STORAGE_MANAGEMENT_TOKEN`). This Secret overrides duplicate keys from `existingSecret`.
- `worker.caSecret`: worker trust bundle; defaults to `caSecret`. Its `ca.crt` must trust every HTTPS upstream used by the worker.
- `internalTLS.enabled` and `internalTLS.existingSecret`: enable the API's Nginx TLS sidecar and Service port 8443. `certificateKey` and `privateKeyKey` select Secret keys. API HTTP port 80 remains available for the existing same-origin frontend.

The chart owns the TLS ConfigMap and sidecar, so Helm upgrades retain them. Changes to TLS configuration trigger a rollout. When rotating externally managed Secret contents, change `configRevision` to restart consumers. The image applies to API, worker, migrate and bootstrap containers together.

Current OrbStack deployment uses the `charts/forge` umbrella chart in the sibling `artifact-go` repository, with release and namespace `forge`. That chart owns installation inputs and initializes tenant, web, Artifact/Storage and their credentials together. The standalone OrbStack overlay for the retired environment has been removed; use the parent chart's deployment inputs.

Before the first upgrade from a manually patched installation, back up the tenant database and existing Helm release. The chart runs the selected image's migrate/bootstrap commands, including when earlier manual patches only changed the main container image. Keep database PVCs and Secrets during cleanup of superseded proxy resources.
