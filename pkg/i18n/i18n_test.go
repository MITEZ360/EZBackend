package i18n

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTfWithContext(t *testing.T) {
	ctx := WithLocale(context.Background(), "en")
	if got := Tf(ctx, "文件大小超过限制（最大 {0}）", "20 MB"); got != "File size exceeds the limit (max 20 MB)" {
		t.Errorf("英文动态消息翻译错误: %q", got)
	}
	if got := Tf(ctx, "未收录的 {0} 模板", "值"); got != "未收录的 值 模板" {
		t.Errorf("未收录模板应仅替换占位符: %q", got)
	}
	zh := WithLocale(context.Background(), "zh-CN")
	if got := Tf(zh, "文件大小超过限制（最大 {0}）", "20 MB"); got != "文件大小超过限制（最大 20 MB）" {
		t.Errorf("中文动态消息应保持原文并替换占位符: %q", got)
	}
}

func TestMiddlewarePropagatesLocaleToRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(Middleware("zh-CN"))
	e.GET("/", func(c *gin.Context) {
		c.String(200, FromContext(c.Request.Context()))
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Locale", "en")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Body.String() != "en" {
		t.Errorf("语言未传播到 request context: %q", w.Body.String())
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"zh-CN": "zh-CN", "zh_CN": "zh-CN", "zh-Hans-CN": "zh-CN", "ZH": "zh-CN",
		"en": "en", "en-US": "en", "en_GB": "en",
		"fr-FR": "", "": "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromAcceptLanguage(t *testing.T) {
	cases := map[string]string{
		"en-US,en;q=0.9,zh;q=0.8": "en",
		"fr-FR, zh-CN;q=0.7":      "zh-CN",
		"fr-FR":                   "",
		"":                        "",
	}
	for in, want := range cases {
		if got := FromAcceptLanguage(in); got != want {
			t.Errorf("FromAcceptLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTranslateFallback(t *testing.T) {
	if got := Translate("zh-CN", "用户名已存在"); got != "用户名已存在" {
		t.Errorf("中文原文应保持不变, got %q", got)
	}
	if got := Translate("en", "用户名已存在"); got != "Username already exists" {
		t.Errorf("英文译文错误, got %q", got)
	}
	// 未收录的文案降级为中文原文，保证渐进翻译不破坏存量接口
	if got := Translate("en", "一个尚未翻译的提示"); got != "一个尚未翻译的提示" {
		t.Errorf("未收录文案应降级为原文, got %q", got)
	}
}

func TestMiddlewarePriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(Middleware("zh-CN"))
	e.GET("/", func(c *gin.Context) { c.String(200, Locale(c)) })

	// 无请求头 -> 默认中文
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Body.String() != "zh-CN" {
		t.Errorf("默认语言错误: %q", w.Body.String())
	}

	// X-Locale 优先于 Accept-Language
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Locale", "zh-CN")
	r.Header.Set("Accept-Language", "en-US")
	w = httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Body.String() != "zh-CN" {
		t.Errorf("X-Locale 优先级错误: %q", w.Body.String())
	}

	// Accept-Language 生效
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	w = httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Body.String() != "en" {
		t.Errorf("Accept-Language 未生效: %q", w.Body.String())
	}
}
