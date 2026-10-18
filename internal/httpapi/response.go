package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/FlyTOmeLight/server-go/constants"
	serrors "github.com/FlyTOmeLight/server-go/errors"
	core "github.com/FlyTOmeLight/server-go/pkg"
	"github.com/FlyTOmeLight/server-go/pkg/binder"
	"github.com/gin-gonic/gin"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

// errorWriter maps framework failures to Forge's established wire contract.
type errorWriter struct{ gin.ResponseWriter }

func (w *errorWriter) Write(data []byte) (int, error) {
	if w.Status() >= 400 && !w.Written() {
		var failure struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &failure) == nil && failure.Code != "" {
			switch failure.Code {
			case "APINotFound":
				failure.Code, failure.Message = "NOT_FOUND", "接口不存在"
			case "MethodNotAllowed":
				failure.Code, failure.Message = "METHOD_NOT_ALLOWED", "请求方法不支持"
			case "Unauthorized":
				failure.Code, failure.Message = "SERVICE_AUTH_REQUIRED", "服务身份校验失败"
			case "InternalError":
				failure.Code, failure.Message = "INTERNAL_ERROR", "服务暂不可用"
			}
			encoded, err := json.Marshal(map[string]any{"error": failure})
			if err != nil {
				return 0, err
			}
			if _, err = w.ResponseWriter.Write(encoded); err != nil {
				return 0, err
			}
			return len(data), nil
		}
	}
	return w.ResponseWriter.Write(data)
}

func (w *errorWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func Install(engine *core.Engine) {
	engine.RedirectTrailingSlash = false
	engine.Use(func(c *core.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Header(constants.HeaderRequestID, c.GetHeader(constants.HeaderRequestID))
		c.Writer = &errorWriter{ResponseWriter: c.Writer}
		c.Next()
	})
	engine.NoRoute(func(c *core.Context) { serrors.Abort(c, 404, "NOT_FOUND", "接口不存在") })
	engine.NoMethod(func(c *core.Context) { serrors.Abort(c, 405, "METHOD_NOT_ALLOWED", "请求方法不支持") })
}

func Fail(c *core.Context, err error) {
	var detail *apperror.Error
	if errors.As(err, &detail) {
		err = serrors.NewError(detail.Status, detail.Code, detail.Message)
	}
	serrors.AbortWithError(c, err)
}

func Bind(c *core.Context, request any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	if err := binder.Bind(c, request).JSON().Error(); err != nil {
		return apperror.New(400, "INVALID_JSON", "请求内容无效，须提供一个 JSON 对象")
	}
	return nil
}

// MutationBody retains the exact request bytes for persistent idempotency.
func MutationBody(c *core.Context, request any) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 65536))
	if err != nil {
		return nil, apperror.New(400, "INVALID_BODY", "请求过大")
	}
	if len(raw) > 0 {
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		if err := Bind(c, request); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// authenticateSession validates the forge_session cookie and the browser CSRF
// contract for writes. A failed check aborts the request.
func authenticateSession(
	c *core.Context,
	authenticate func(context.Context, string) (identity.User, string, error),
	publicURL string,
) (identity.User, string, bool) {
	session, _ := c.Cookie("forge_session")
	u, csrf, err := authenticate(c.Request.Context(), session)
	if err != nil {
		Fail(c, err)
		return identity.User{}, "", false
	}
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" &&
		(c.GetHeader("Origin") != publicURL || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(csrf)) != 1) {
		Fail(c, apperror.New(403, "CSRF_REJECTED", "请求来源校验失败，请刷新页面"))
		return identity.User{}, "", false
	}
	return u, csrf, true
}

// bearerToken returns the credential carried by the Bearer scheme. It reports
// false when the header is absent; an empty credential stays the caller's 401.
func bearerToken(header string) (string, bool) {
	if header == "Bearer" {
		return "", true
	}
	value, ok := strings.CutPrefix(header, "Bearer ")
	return value, ok
}

// Session authenticates the browser session cookie only. API keys are
// rejected so a stolen key cannot manage credentials or terminate sessions.
func Session(
	authenticate func(context.Context, string) (identity.User, string, error),
	publicURL string,
) core.HandlerFunc {
	return func(c *core.Context) {
		u, csrf, ok := authenticateSession(c, authenticate, publicURL)
		if !ok {
			return
		}
		c.Set("forge.user", u)
		c.Set("forge.csrf", csrf)
		c.Next()
	}
}

// Identity accepts exactly one credential: the browser session cookie or a
// user Bearer API key. Cookie requests keep the CSRF contract; API key calls
// are non-browser traffic and skip it.
func Identity(
	authenticate func(context.Context, string) (identity.User, string, error),
	apiKey func(context.Context, string) (identity.User, error),
	publicURL string,
) core.HandlerFunc {
	return func(c *core.Context) {
		token, hasBearer := bearerToken(c.GetHeader("Authorization"))
		session, _ := c.Cookie("forge_session")
		if hasBearer {
			if session != "" {
				Fail(c, apperror.New(400, "AMBIGUOUS_IDENTITY_PROOF", "身份证明只能使用一种凭据"))
				return
			}
			if token == "" {
				Fail(c, apperror.New(401, "AUTH_REQUIRED", "API key 无效"))
				return
			}
			u, err := apiKey(c.Request.Context(), token)
			if err != nil {
				Fail(c, err)
				return
			}
			c.Set("forge.user", u)
			c.Next()
			return
		}
		u, _, ok := authenticateSession(c, authenticate, publicURL)
		if !ok {
			return
		}
		c.Set("forge.user", u)
		c.Next()
	}
}

// ServiceAuth authenticates internal callers with their own service API key.
func ServiceAuth(authenticate func(context.Context, string) (string, error)) core.HandlerFunc {
	return func(c *core.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok || token == "" {
			Fail(c, apperror.New(401, "SERVICE_AUTH_REQUIRED", "服务身份校验失败"))
			return
		}
		service, err := authenticate(c.Request.Context(), token)
		if err != nil {
			Fail(c, err)
			return
		}
		c.Set("forge.service", service)
		c.Next()
	}
}

func User(c *core.Context) identity.User {
	value, _ := c.Get("forge.user")
	return value.(identity.User)
}
