package recharge

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/internal/middleware"
	"github.com/MITEZ360/tenon/internal/model"
	"github.com/MITEZ360/tenon/internal/platform/httpx"
	"github.com/MITEZ360/tenon/pkg/query"
	"github.com/MITEZ360/tenon/pkg/response"
	"github.com/MITEZ360/tenon/pkg/utils"
)

// Handler 充值接口。
type Handler struct {
	svc        *Service
	trustProxy bool
}

// NewHandler 创建处理器。
func NewHandler(svc *Service, trustProxy bool) *Handler {
	return &Handler{svc: svc, trustProxy: trustProxy}
}

// Available 返回可用支付渠道。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Available
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/channels [get]
// KE:SWAGGER-END
func (h *Handler) Available(c *gin.Context) {
	response.OK(c, gin.H{"channels": h.svc.Available()})
}

// Create 创建充值订单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Create
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/orders [post]
// KE:SWAGGER-END
func (h *Handler) Create(c *gin.Context) {
	var in CreateInput
	if !httpx.BindJSON(c, &in) {
		return
	}
	order, err := h.svc.CreateOrder(c.Request.Context(), middleware.UserID(c), &in, utils.ClientIP(c, h.trustProxy))
	if err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "recharge", "order.create")
	response.OK(c, order)
}

// Query 查询订单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Query
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param orderNo path string true "orderNo"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/orders/{orderNo} [get]
// KE:SWAGGER-END
func (h *Handler) Query(c *gin.Context) {
	order, err := h.svc.Query(c.Request.Context(), middleware.UserID(c), c.Param("orderNo"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, order)
}

// List 我的订单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary List
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/orders [get]
// KE:SWAGGER-END
func (h *Handler) List(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.List(c.Request.Context(), middleware.UserID(c), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Close 关闭待支付订单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary Close
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param orderNo path string true "orderNo"
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/orders/{orderNo}/close [post]
// KE:SWAGGER-END
func (h *Handler) Close(c *gin.Context) {
	if err := h.svc.Close(c.Request.Context(), middleware.UserID(c), c.Param("orderNo")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"message": "订单已关闭"})
}

// AdminList 管理端全量订单。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary AdminList
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/admin/orders [get]
// KE:SWAGGER-END
func (h *Handler) AdminList(c *gin.Context) {
	p := query.Parse(c)
	list, total, err := h.svc.AdminList(c.Request.Context(), p)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.PageOK(c, list, total, p.Page, p.PageSize)
}

// Notify 支付回调（公开）。先读原始 body 用于验签，再按渠道解析。
func (h *Handler) notify(payType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		form := map[string]string{}
		for k, vals := range c.Request.URL.Query() {
			if len(vals) > 0 {
				form[k] = vals[0]
			}
		}
		var body []byte
		if payType == model.PayAlipay {
			// 支付宝以 application/x-www-form-urlencoded 回调，ParseForm 必须在读 body 之前。
			if err := c.Request.ParseForm(); err != nil {
				c.String(http.StatusBadRequest, "fail")
				return
			}
			for k, vals := range c.Request.PostForm {
				if len(vals) > 0 {
					form[k] = vals[0]
				}
			}
			body = []byte(c.Request.PostForm.Encode())
		} else {
			var err error
			body, err = io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
			if err != nil {
				c.String(http.StatusBadRequest, "fail")
				return
			}
		}
		ct, resp, err := h.svc.Notify(c.Request.Context(), payType, body, form)
		if err != nil {
			c.String(http.StatusBadRequest, "fail")
			return
		}
		c.Data(http.StatusOK, ct, []byte(resp))
	}
}

// WechatNotify 微信支付回调。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary WechatNotify
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/pay/notify/wechat [post]
// KE:SWAGGER-END
func (h *Handler) WechatNotify(c *gin.Context) { h.notify(model.PayWechat)(c) }

// AlipayNotify 支付宝回调。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary AlipayNotify
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /api/v1/pay/notify/alipay [post]
// KE:SWAGGER-END
func (h *Handler) AlipayNotify(c *gin.Context) { h.notify(model.PayAlipay)(c) }

// PaymentConfig 读取管理端支付配置，敏感字段以 ****** 占位。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary PaymentConfig
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/admin/payment-config [get]
// KE:SWAGGER-END
func (h *Handler) PaymentConfig(c *gin.Context) {
	view, err := h.svc.PaymentConfig(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, view)
}

// SavePaymentConfig 保存管理端支付配置，保存后下一次下单/回调立即生效。
// KE:SWAGGER-BEGIN 本接口注解由路由表自动生成，字段说明见 docs/api.md
// @Summary SavePaymentConfig
// @Tags 用户与权限
// @Accept json
// @Produce json
// @Param body body map[string]interface{} false "请求参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 409 {object} map[string]interface{}
// @Failure 429 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /api/v1/recharge/admin/payment-config [put]
// KE:SWAGGER-END
func (h *Handler) SavePaymentConfig(c *gin.Context) {
	var in PaymentConfigView
	if !httpx.BindJSON(c, &in) {
		return
	}
	if err := h.svc.SavePaymentConfig(c.Request.Context(), &in); err != nil {
		response.Fail(c, err)
		return
	}
	middleware.AuditInfo(c, "recharge", "payment-config.update")
	response.OK(c, gin.H{"message": "支付配置已保存并立即生效"})
}
