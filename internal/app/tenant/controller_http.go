package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/FlyTOmeLight/server-go/conf"
	"github.com/FlyTOmeLight/server-go/constants"
	serrors "github.com/FlyTOmeLight/server-go/errors"
	"github.com/FlyTOmeLight/server-go/pkg/binder"
	"github.com/gin-gonic/gin"
)

// httpWriter preserves Forge's public error envelope at the framework boundary.
// Successful responses, redirects and identity proxy bodies pass through unchanged.
type httpWriter struct {
	gin.ResponseWriter
	context *gin.Context
}

func (w *httpWriter) Write(data []byte) (int, error) {
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
			encoded, err := json.Marshal(Object{"error": failure})
			if err != nil {
				return 0, err
			}
			_, err = w.ResponseWriter.Write(encoded)
			if err != nil {
				return 0, err
			}
			return len(data), nil
		}
	}
	return w.ResponseWriter.Write(data)
}

func (w *httpWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// SetupHTTP installs the API's protocol policy; HTTPHost owns common middleware.
func (a *Controller) SetupHTTP(engine *gin.Engine, _ *conf.Config) error {
	engine.RedirectTrailingSlash = false
	engine.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "no-store")
		c.Header(constants.HeaderRequestID, c.GetHeader(constants.HeaderRequestID))
		c.Writer = &httpWriter{ResponseWriter: c.Writer, context: c}
		c.Next()
	})
	// Reinstall fallback handlers so they also pass through the Forge envelope.
	engine.NoRoute(func(c *gin.Context) { serrors.Abort(c, 404, "NOT_FOUND", "接口不存在") })
	engine.NoMethod(func(c *gin.Context) { serrors.Abort(c, 405, "METHOD_NOT_ALLOWED", "请求方法不支持") })
	return nil
}

func route(group *gin.RouterGroup, method, path string, handler http.HandlerFunc) {
	group.Handle(method, path, func(c *gin.Context) {
		for _, param := range c.Params {
			c.Request.SetPathValue(param.Key, param.Value)
		}
		handler(c.Writer, c.Request)
	})
}

// Readiness additionally checks PostgreSQL; /healthz is owned by HTTPHost.
func (a *Controller) readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.service.Readiness(ctx); err != nil {
		JSON(w, 503, Object{"status": "unavailable"})
		return
	}
	JSON(w, 200, Object{"status": "ok"})
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	// Retain the tenant API's 64 KiB input boundary; binding is owned by server-go.
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	if err := binder.Bind(w.(*httpWriter).context, v).JSON().Error(); err != nil {
		return problem(400, "INVALID_JSON", "请求内容无效，须提供一个 JSON 对象")
	}
	return nil
}

func fail(w http.ResponseWriter, err error) {
	var detail *Error
	if errors.As(err, &detail) {
		err = serrors.NewError(detail.Status, detail.Code, detail.Message)
	}
	serrors.AbortWithError(w.(*httpWriter).context, err)
}
