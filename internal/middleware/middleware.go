// Package middleware 提供 Gin 中间件：链路追踪、访问日志、异常恢复、跨域、安全头、限流、
// 认证鉴权与操作审计。
package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"github.com/keel-framework/keel/internal/platform/config"
	"github.com/keel-framework/keel/pkg/response"
	"github.com/keel-framework/keel/pkg/utils"
)

// Trace 注入链路 ID（优先复用上游传入），并写入响应头便于前端与网关串联。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Trace-Id")
		if traceID == "" {
			if tp := c.GetHeader("traceparent"); tp != "" {
				parts := strings.Split(tp, "-")
				if len(parts) >= 3 {
					traceID = parts[2]
				}
			}
		}
		if traceID == "" {
			traceID = uuid.NewString()
		}
		c.Set("trace_id", traceID)
		c.Header("X-Trace-Id", traceID)
		c.Next()
	}
}

// AccessLog 输出结构化访问日志，慢请求自动升级为 Warn。
func AccessLog(log *zap.Logger, slowThreshold time.Duration) gin.HandlerFunc {
	skip := []string{"/healthz", "/readyz", "/metrics", "/favicon.ico", "/uploads/"}
	return func(c *gin.Context) {
		for _, p := range skip {
			if strings.HasPrefix(c.Request.URL.Path, p) {
				c.Next()
				return
			}
		}
		start := time.Now()
		c.Next()
		cost := time.Since(start)
		fields := []zap.Field{
			zap.String("trace_id", c.GetString("trace_id")),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("route", c.FullPath()),
			zap.Int("status", c.Writer.Status()),
			zap.Int64("cost_ms", cost.Milliseconds()),
			zap.Int("bytes", c.Writer.Size()),
			zap.String("ip", utils.ClientIP(c, true)),
			zap.String("ua", c.Request.UserAgent()),
		}
		if uid := c.GetString("user_id"); uid != "" {
			fields = append(fields, zap.String("user_id", uid))
		}
		switch {
		case c.Writer.Status() >= 500:
			log.Error("请求处理异常", fields...)
		case c.Writer.Status() >= 400:
			log.Warn("请求被拒绝", fields...)
		case slowThreshold > 0 && cost > slowThreshold:
			log.Warn("慢请求", fields...)
		default:
			log.Info("请求完成", fields...)
		}
	}
}

// Recovery 捕获 panic，记录堆栈并返回统一错误响应。
func Recovery(log *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(gin.DefaultErrorWriter, func(c *gin.Context, rec any) {
		log.Error("请求触发 panic",
			zap.Any("panic", rec),
			zap.String("trace_id", c.GetString("trace_id")),
			zap.String("path", c.Request.URL.Path),
			zap.String("method", c.Request.Method),
			zap.String("ip", utils.ClientIP(c, true)),
			zap.Stack("stack"),
		)
		if c.Writer.Written() {
			c.Abort()
			return
		}
		response.FailMsg(c, http.StatusInternalServerError, 50000, "服务内部错误，请联系管理员")
	})
}

// CORS 跨域配置。
func CORS(cfg config.CORS) gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     cfg.AllowOrigins,
		AllowMethods:     cfg.AllowMethods,
		AllowHeaders:     cfg.AllowHeaders,
		ExposeHeaders:    cfg.ExposeHeaders,
		AllowCredentials: cfg.AllowCredentials,
		MaxAge:           time.Duration(cfg.MaxAge) * time.Second,
	})
}

// SecurityHeaders 写入浏览器安全响应头。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-XSS-Protection", "1; mode=block")
		if c.Request.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// limiter 是按 key 隔离的令牌桶集合，定期清理空闲条目防止内存膨胀。
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rps     rate.Limit
	burst   int
	lastGC  time.Time
}

type bucket struct {
	l *rate.Limiter
	t time.Time
}

func newLimiter(rps float64, burst int) *limiter {
	if rps <= 0 {
		rps = 100
	}
	if burst <= 0 {
		burst = int(rps * 2)
	}
	return &limiter{buckets: map[string]*bucket{}, rps: rate.Limit(rps), burst: burst, lastGC: time.Now()}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.lastGC) > 5*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.t) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{l: rate.NewLimiter(l.rps, l.burst)}
		l.buckets[key] = b
	}
	b.t = now
	return b.l.Allow()
}

// RateLimit 全局限流，支持按用户或 IP 维度。
func RateLimit(cfg config.RateLimit, trustProxy bool) gin.HandlerFunc {
	if !cfg.Enabled {
		return func(c *gin.Context) { c.Next() }
	}
	lim := newLimiter(cfg.RPS, cfg.Burst)
	return func(c *gin.Context) {
		key := utils.ClientIP(c, trustProxy)
		if cfg.ByUser {
			if uid := c.GetString("user_id"); uid != "" {
				key = "u:" + uid
			}
		}
		if !lim.allow(key) {
			c.Header("Retry-After", "1")
			response.FailMsg(c, http.StatusTooManyRequests, 42900, "请求过于频繁，请稍后再试")
			return
		}
		c.Next()
	}
}

// NoCache 禁用接口缓存。
func NoCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Header("Pragma", "no-cache")
		c.Next()
	}
}

// InternalOnly 限制仅内网/服务间调用可访问（校验内部令牌）。
func InternalOnly(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" || c.GetHeader("X-Internal-Token") != token {
			response.FailMsg(c, http.StatusForbidden, 40300, "禁止访问")
			return
		}
		c.Next()
	}
}

// BodyLimit 限制请求体大小。
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if max > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		}
		c.Next()
	}
}

// Timeout 通过上下文超时约束下游调用（数据库、RPC 会感知该 deadline），
// 超时且尚未写出响应时返回 503。
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		if ctx.Err() == context.DeadlineExceeded && !c.Writer.Written() {
			response.FailMsg(c, http.StatusServiceUnavailable, 50300, "请求处理超时")
		}
	}
}
