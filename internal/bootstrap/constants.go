package bootstrap

import "time"

const (
	Timeout           = 300 * time.Second
	AdminDirectory    = "/bootstrap-admin"
	SeedDirectory     = "/identity-seed"
	ExternalDirectory = "/external"

	CompleteKey            = "BOOTSTRAP_COMPLETE"
	casdoorClientIDKey     = "CASDOOR_CLIENT_ID"
	casdoorSecretKey       = "CASDOOR_SECRET"
	oidcClientIDKey        = "OIDC_CLIENT_ID"
	oidcClientSecretKey    = "OIDC_CLIENT_SECRET"
	bootstrapUsernameKey   = "BOOTSTRAP_USERNAME"
	bootstrapPasswordKey   = "BOOTSTRAP_PASSWORD"
	gitLabTokenKey         = "GITLAB_TOKEN"
	gitLabTokenIDKey       = "GITLAB_TOKEN_ID"
	gitLabTokenUserIDKey   = "GITLAB_TOKEN_USER_ID"
	gitLabTokenNameKey     = "GITLAB_TOKEN_NAME"
	gitLabIssueAttemptsKey = "GITLAB_ISSUE_ATTEMPTS"

	gitLabScope             = "api"
	gitLabDescriptionPrefix = "Forge tenant bootstrap namespace="
)

func gitLabTokenDescription(namespace string) string {
	return gitLabDescriptionPrefix + namespace
}
