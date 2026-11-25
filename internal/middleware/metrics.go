package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tenon",
		Name:      "http_requests_total",
		Help:      "HTTP 请求总数",
	}, []string{"method", "route", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "tenon",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP 请求耗时分布",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "route"})

	httpInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "tenon",
		Name:      "http_inflight_requests",
		Help:      "当前处理中的请求数",
	})

	businessOps = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "tenon",
		Name:      "business_operations_total",
		Help:      "业务操作计数（按模块与结果）",
	}, []string{"module", "result"})
)

// Metrics 采集 HTTP 指标。route 使用注册模板而非原始路径，避免标签基数爆炸。
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		httpInflight.Inc()
		defer httpInflight.Dec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())
		httpRequests.WithLabelValues(c.Request.Method, route, status).Inc()
		httpDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}

// MetricsHandler 暴露 Prometheus 抓取端点，token 非空时需要 Bearer 校验。
func MetricsHandler(token string) gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		if token != "" && c.GetHeader("Authorization") != "Bearer "+token {
			c.String(401, "unauthorized")
			c.Abort()
			return
		}
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// ObserveBusiness 记录业务操作结果，供自定义指标面板使用。
func ObserveBusiness(module, result string) {
	businessOps.WithLabelValues(module, result).Inc()
}
