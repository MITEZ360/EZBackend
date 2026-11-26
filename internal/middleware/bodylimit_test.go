package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestBodyLimitDeclaredLength 声明的 Content-Length 超限时直接 413，
// 不必等解码到一半才失败——解码中途超限只会得到 "unexpected EOF"，
// 与"JSON 没写完整"无法区分。
func TestBodyLimitDeclaredLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(16))
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	do := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	over := do(`{"a":"` + strings.Repeat("x", 64) + `"}`)
	if over.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超限请求应 413，实际 %d %s", over.Code, over.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(over.Body.Bytes(), &payload); err != nil {
		t.Fatalf("413 响应不是 JSON: %v", err)
	}
	if code, _ := payload["code"].(float64); int(code) != 41300 {
		t.Fatalf("业务码应为 41300，实际 %v", payload["code"])
	}

	if w := do(`{"a":1}`); w.Code != http.StatusOK {
		t.Fatalf("限制之内的请求应放行，实际 %d", w.Code)
	}

	// 未设置上限（<=0）时不拦截。
	r2 := gin.New()
	r2.Use(BodyLimit(0))
	r2.POST("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(strings.Repeat("x", 4096)))
	w := httptest.NewRecorder()
	r2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("未配置上限时不应拦截，实际 %d", w.Code)
	}
}
