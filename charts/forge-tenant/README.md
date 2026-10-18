# Tenant Helm deployment

`existingSecret` supplies the application environment; `caSecret` supplies the trust bundle at `/ca`. Credentials and private keys remain in existing Kubernetes Secrets and are not rendered into Helm values.

For Artifact integration, configure:

- `worker.existingSecret`: additional worker-only environment (the deployed integration uses `STORAGE_URL` and `STORAGE_MANAGEMENT_TOKEN`). This Secret overrides duplicate keys from `existingSecret`.
- `worker.caSecret`: worker trust bundle; defaults to `caSecret`. Its `ca.crt` must trust every HTTPS upstream used by the worker.
- `internalTLS.enabled` and `internalTLS.existingSecret`: enable the API's Nginx TLS sidecar and Service port 8443. `certificateKey` and `privateKeyKey` select Secret keys. API HTTP port 80 remains available for the existing same-origin frontend.

The chart owns the TLS ConfigMap and sidecar, so Helm upgrades retain them. Changes to TLS configuration trigger a rollout. When rotating externally managed Secret contents, change `configRevision` to restart consumers. The image applies to API, worker, migrate and bootstrap containers together.

The non-secret OrbStack overlay belongs to this repository at `charts/forge-tenant/values-orbstack.yaml`. It configures only the tenant release; Artifact and frontend releases are deployed by their own repositories. Existing integration Secret names refer to the current shared namespace and do not transfer ownership of those Secrets to this chart. Validate and deploy from the root of this repository:

```sh
helm lint charts/forge-tenant -f charts/forge-tenant/values-orbstack.yaml
helm upgrade --install forge-tenant charts/forge-tenant \
  --kube-context orbstack --namespace forge-tenant-split \
  --reset-values -f charts/forge-tenant/values-orbstack.yaml --wait --timeout 5m
```

Before the first upgrade from a manually patched installation, back up the tenant database and existing Helm release. The chart runs the selected image's migrate/bootstrap commands, including when earlier manual patches only changed the main container image. Keep database PVCs and Secrets during cleanup of superseded proxy resources.
