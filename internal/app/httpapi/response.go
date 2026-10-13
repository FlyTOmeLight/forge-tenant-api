package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/FlyTOmeLight/server-go/constants"
	serrors "github.com/FlyTOmeLight/server-go/errors"
	"github.com/FlyTOmeLight/server-go/pkg/binder"
	"github.com/gin-gonic/gin"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
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

func Install(engine *gin.Engine) {
	engine.RedirectTrailingSlash = false
	engine.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Header(constants.HeaderRequestID, c.GetHeader(constants.HeaderRequestID))
		c.Writer = &errorWriter{ResponseWriter: c.Writer}
		c.Next()
	})
	engine.NoRoute(func(c *gin.Context) { serrors.Abort(c, 404, "NOT_FOUND", "接口不存在") })
	engine.NoMethod(func(c *gin.Context) { serrors.Abort(c, 405, "METHOD_NOT_ALLOWED", "请求方法不支持") })
}

func Fail(c *gin.Context, err error) {
	var detail *apperror.Error
	if errors.As(err, &detail) {
		err = serrors.NewError(detail.Status, detail.Code, detail.Message)
	}
	serrors.AbortWithError(c, err)
}

func Bind(c *gin.Context, request any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	if err := binder.Bind(c, request).JSON().Error(); err != nil {
		return apperror.New(400, "INVALID_JSON", "请求内容无效，须提供一个 JSON 对象")
	}
	return nil
}

// MutationBody retains the exact request bytes for persistent idempotency.
func MutationBody(c *gin.Context, request any) ([]byte, error) {
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

func Session(
	authenticate func(context.Context, string) (identity.User, string, error),
	publicURL string,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		session, _ := c.Cookie("forge_session")
		u, csrf, err := authenticate(c.Request.Context(), session)
		if err != nil {
			Fail(c, err)
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" &&
			(c.GetHeader("Origin") != publicURL || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(csrf)) != 1) {
			Fail(c, apperror.New(403, "CSRF_REJECTED", "请求来源校验失败，请刷新页面"))
			return
		}
		c.Set("forge.user", u)
		c.Set("forge.csrf", csrf)
		c.Next()
	}
}

func User(c *gin.Context) identity.User {
	value, _ := c.Get("forge.user")
	return value.(identity.User)
}
