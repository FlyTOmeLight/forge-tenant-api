package tenantwrite

import (
	"context"

	core "github.com/FlyTOmeLight/server-go/pkg"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/httpapi"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

// Execute binds a tenant write request and preserves the stored replay response.
func Execute[T any](c *core.Context, fn func(context.Context, identity.User, Change, T) (Receipt, error)) {
	var req T
	raw, err := httpapi.MutationBody(c, &req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	change := Change{
		TenantID:    c.Param("tid"),
		ProjectID:   c.Param("pid"),
		MemberID:    c.Param("mid"),
		TeamID:      c.Param("team"),
		OperationID: c.Param("oid"),
		Remove:      c.Request.Method == "DELETE",
		Key:         c.GetHeader("Idempotency-Key"),
		Version:     c.GetHeader("If-Match"),
		Route:       c.Request.Method + " " + c.Request.URL.Path,
		Target:      c.Request.URL.Path,
		Raw:         raw,
	}
	result, err := fn(c.Request.Context(), httpapi.User(c), change, req)
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	c.Data(result.Status, "application/json", result.Data)
}
