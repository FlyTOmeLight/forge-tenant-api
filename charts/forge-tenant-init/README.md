# Forge external initialization

The Forge umbrella chart includes this chart as dependency `bootstrap`. It owns the administrator input Secret (unless `bootstrap.existingSecret` is set), ServiceAccount, Role, RoleBinding and ordinary initialization Job. The parent retains ownership of `<release>-identity-seed`, `<release>-external-credentials` and the shared CA Secret. The CA Secret defaults to `<release>-external-ca`; `global.forge.external.caSecret` selects an existing Secret instead.

Set administrator credentials through `bootstrap.existingSecret`, containing `CASDOOR_ADMIN_CLIENT_ID`, `CASDOOR_ADMIN_CLIENT_SECRET` and `GITLAB_ADMIN_TOKEN`, or through `bootstrap.authorization.casdoorClientID`, `casdoorClientSecret` and `gitlabToken`. The input is mounted only into the Job at `/bootstrap-admin`; seed material is mounted at `/identity-seed`. Do not commit real credentials to values files.

Both this Job and the Tenant chart use the parent-provided `global.forge.tenantImage` (`repository`, `tag`, `pullPolicy`). The image must contain `/forge-init` and the main `conf/server/config.toml`. The Job runs `/forge-init external-bootstrap`, and explicitly injects the following TOML inputs without loading a Tenant runtime Secret:

| Parent value | Environment variable |
| --- | --- |
| `global.forge.external.casdoorURL` | `CASDOOR_URL` |
| `global.forge.external.organization` | `CASDOOR_ORGANIZATION` |
| `global.forge.external.gitlabURL` | `GITLAB_URL` |
| `global.forge.publicURL` | `PUBLIC_URL` |
| `global.forge.external.certificate` | `BOOTSTRAP_CERTIFICATE` |
| `global.forge.bootstrap.username` | `BOOTSTRAP_USERNAME` |
| `global.forge.bootstrap.email` | `BOOTSTRAP_EMAIL` |
| `global.forge.bootstrap.gitlabTokenExpiresAt` | `GITLAB_TOKEN_EXPIRES_AT` |

`CA_FILE` is `/external-ca/ca.crt`. `POD_NAMESPACE` comes from the Pod's namespace through the Downward API; `HELM_RELEASE_NAME` comes from Helm. The binary derives stable external identity and output Secret names from these two values.

The Job name remains `<release>-external-bootstrap-<revision>`, with two retries, a fixed 300-second execution budget and a 360-second Job deadline. Its Role grants only `get` and `patch` on `<release>-external-credentials`. Successful Jobs expire after one day. This is an ordinary Job, so Tenant's `/forge-init wait-external` init containers can wait for the output Secret while Helm waits for Pods. Use `helm upgrade --install --wait --wait-for-jobs` to also require the current Job to finish; an existing complete output Secret can already unblock business Pods during upgrades.
