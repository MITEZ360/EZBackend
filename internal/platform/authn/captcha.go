package authn

import (
	"context"
	"time"

	"github.com/mojocn/base64Captcha"

	"github.com/keel-framework/keel/internal/platform/cache"
)

// captchaTTL 图形验证码有效期。
// 覆盖两步登录：密码通过后还要留出输入动态码的时间。
const captchaTTL = 5 * time.Minute

// cacheStore 将图形验证码存入统一缓存，支持多实例共享。
type cacheStore struct {
	c      cache.Cache
	prefix string
}

func (s *cacheStore) Set(id, value string) error {
	return s.c.Set(context.Background(), s.prefix+id, []byte(value), captchaTTL)
}

func (s *cacheStore) Get(id string, clear bool) string {
	ctx := context.Background()
	b, err := s.c.Get(ctx, s.prefix+id)
	if clear {
		_ = s.c.Delete(ctx, s.prefix+id)
	}
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *cacheStore) Verify(id, answer string, clear bool) bool {
	v := s.Get(id, clear)
	return v != "" && v == answer
}

// Captcha 图形验证码服务（数字运算型，4 位）。
type Captcha struct {
	cp    *base64Captcha.Captcha
	store base64Captcha.Store
}

// NewCaptcha 创建验证码服务。
func NewCaptcha(c cache.Cache) *Captcha {
	driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	store := &cacheStore{c: c, prefix: "captcha:"}
	return &Captcha{cp: base64Captcha.NewCaptcha(driver, store), store: store}
}

// Generate 生成验证码，返回 ID 与 base64 PNG（可直接作为 img src）。
func (c *Captcha) Generate() (id, b64 string, err error) {
	id, b64, _, err = c.cp.Generate()
	return id, b64, err
}

// Verify 校验验证码，校验后立即失效。
func (c *Captcha) Verify(id, answer string) bool {
	if id == "" || answer == "" {
		return false
	}
	return c.cp.Verify(id, answer, true)
}

// Peek 只校验不消费，用于两步登录：密码通过时还需保留验证码给第二次提交。
func (c *Captcha) Peek(id, answer string) bool {
	if id == "" || answer == "" {
		return false
	}
	return c.cp.Verify(id, answer, false)
}

// Drop 主动作废验证码。
func (c *Captcha) Drop(id string) {
	if id == "" {
		return
	}
	c.store.Get(id, true)
}
