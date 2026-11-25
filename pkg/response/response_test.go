package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MITEZ360/tenon/pkg/i18n"
)

func TestOKTranslatesDataMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(i18n.Middleware("zh-CN"))
	e.GET("/", func(c *gin.Context) {
		OK(c, gin.H{"message": "已保存", "id": "1"})
	})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Locale", "en")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)

	var body struct {
		Message string `json:"message"`
		Data    struct {
			Message string `json:"message"`
			ID      string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应解析失败: %v (%s)", err, w.Body.String())
	}
	if body.Message != "Success" {
		t.Errorf("顶层文案未翻译: %q", body.Message)
	}
	if body.Data.Message != "Saved" {
		t.Errorf("data.message 未翻译: %q", body.Data.Message)
	}
	if body.Data.ID != "1" {
		t.Errorf("其他数据字段被改动: %q", body.Data.ID)
	}
}

func TestOKKeepsDataMessageInChinese(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(i18n.Middleware("zh-CN"))
	e.GET("/", func(c *gin.Context) { OK(c, gin.H{"message": "已保存"}) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := w.Body.String(); !contains(got, `"message":"已保存"`) {
		t.Errorf("中文模式应保持原文: %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
