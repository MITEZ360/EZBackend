// Package httpx 提供 Gin 参数绑定与校验错误中文化，统一处理请求解析失败。
package httpx

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zhtrans "github.com/go-playground/validator/v10/translations/zh"

	"github.com/keel-framework/keel/pkg/errs"
	"github.com/keel-framework/keel/pkg/response"
)

var (
	validate *validator.Validate
	trans    ut.Translator
	rePhone  = regexp.MustCompile(`^1[3-9]\d{9}$`)
	reUser   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]{2,31}$`)
)

func init() {
	validate = validator.New()
	// 框架内所有结构体统一使用 binding 标签，校验器必须按同名标签解析，
	// 否则自定义校验规则（username/mobile/password）不会生效。
	validate.SetTagName("binding")
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		// label 优先，使校验提示直接使用中文字段名
		for _, tag := range []string{"label", "json", "form"} {
			name := strings.SplitN(fld.Tag.Get(tag), ",", 2)[0]
			if name != "" && name != "-" {
				return name
			}
		}
		return fld.Name
	})
	_ = validate.RegisterValidation("mobile", func(fl validator.FieldLevel) bool {
		return rePhone.MatchString(fl.Field().String())
	})
	_ = validate.RegisterValidation("username", func(fl validator.FieldLevel) bool {
		return reUser.MatchString(fl.Field().String())
	})
	_ = validate.RegisterValidation("password", func(fl validator.FieldLevel) bool {
		return len(fl.Field().String()) >= 8
	})

	zhLocale := zh.New()
	uni := ut.New(zhLocale, zhLocale)
	trans, _ = uni.GetTranslator("zh")
	_ = zhtrans.RegisterDefaultTranslations(validate, trans)
	registerExtra(validate, trans)

	// 用带中文翻译的校验器替换 Gin 默认校验器，使 binding 错误可直接翻译
	binding.Validator = &validatorWrapper{v: validate}
}

func registerExtra(v *validator.Validate, t ut.Translator) {
	_ = v.RegisterTranslation("mobile", t, func(ut ut.Translator) error {
		return ut.Add("mobile", "{0}格式不正确", true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		s, _ := ut.T("mobile", fe.Field())
		return s
	})
	_ = v.RegisterTranslation("username", t, func(ut ut.Translator) error {
		return ut.Add("username", "{0}需以字母开头，仅含字母、数字、下划线，长度 3-32", true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		s, _ := ut.T("username", fe.Field())
		return s
	})
	_ = v.RegisterTranslation("password", t, func(ut ut.Translator) error {
		return ut.Add("password", "{0}长度至少 8 位", true)
	}, func(ut ut.Translator, fe validator.FieldError) string {
		s, _ := ut.T("password", fe.Field())
		return s
	})
}

type validatorWrapper struct{ v *validator.Validate }

func (w *validatorWrapper) ValidateStruct(obj any) error {
	if obj == nil {
		return nil
	}
	return w.v.Struct(obj)
}

func (w *validatorWrapper) Engine() any { return w.v }

// Bind 解析 JSON/表单/查询参数并校验；失败时直接输出 422 响应并返回 false。
func Bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBind(dst); err != nil {
		response.Fail(c, ValidationError(err))
		return false
	}
	if err := validate.Struct(dst); err != nil {
		response.Fail(c, ValidationError(err))
		return false
	}
	return true
}

// BindJSON 只解析 JSON。
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		response.Fail(c, ValidationError(err))
		return false
	}
	if err := validate.Struct(dst); err != nil {
		response.Fail(c, ValidationError(err))
		return false
	}
	return true
}

// BindQuery 只解析查询字符串。
func BindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		response.Fail(c, ValidationError(err))
		return false
	}
	if err := validate.Struct(dst); err != nil {
		response.Fail(c, ValidationError(err))
		return false
	}
	return true
}

// ValidationError 将 validator 错误转换为带字段明细的业务错误。
func ValidationError(err error) error {
	if err == nil {
		return nil
	}
	var ve validator.ValidationErrors
	if ok := asValidationErrors(err, &ve); ok {
		details := make(map[string]string, len(ve))
		msgs := make([]string, 0, len(ve))
		for _, fe := range ve {
			msg := fe.Error()
			if trans != nil {
				if m, e := trans.T(fe.Tag(), fe.Field(), fe.Param()); e == nil {
					msg = m
				}
			}
			details[fe.Field()] = msg
			msgs = append(msgs, msg)
		}
		first := "参数校验失败"
		if len(msgs) > 0 {
			first = msgs[0]
		}
		return errs.Validation(first).WithDetails(details)
	}
	if strings.Contains(err.Error(), "EOF") {
		return errs.BadRequest("请求体不能为空")
	}
	if strings.Contains(err.Error(), "invalid character") || strings.Contains(err.Error(), "cannot unmarshal") {
		return errs.BadRequest("请求参数格式错误")
	}
	return errs.BadRequest("请求参数解析失败: " + err.Error())
}

func asValidationErrors(err error, target *validator.ValidationErrors) bool {
	ve, ok := err.(validator.ValidationErrors)
	if !ok {
		return false
	}
	*target = ve
	return true
}

// PasswordStrength 校验密码复杂度，level: 1 长度 2 字母+数字 3 四类字符。
func PasswordStrength(pw string, minLen, level int) error {
	if minLen <= 0 {
		minLen = 8
	}
	if len([]rune(pw)) < minLen {
		return errs.BadRequest(fmt.Sprintf("密码长度至少 %d 位", minLen))
	}
	if level <= 1 {
		return nil
	}
	var up, low, digit, special bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			low = true
		case unicode.IsDigit(r):
			digit = true
		default:
			special = true
		}
	}
	kinds := 0
	for _, b := range []bool{up, low, digit, special} {
		if b {
			kinds++
		}
	}
	if level == 2 && (kinds < 2 || !digit) {
		return errs.BadRequest("密码需同时包含字母与数字")
	}
	if level >= 3 && kinds < 4 {
		return errs.BadRequest("密码需包含大写字母、小写字母、数字与特殊符号")
	}
	return nil
}

// QueryInt 读取整型查询参数。
func QueryInt(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// NoContent 返回 204。
func NoContent(c *gin.Context) { c.Status(http.StatusNoContent) }
