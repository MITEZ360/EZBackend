// Package errs 定义统一错误模型与业务错误码。
// 约定：业务码 = HTTP 状态码 * 100 + 序号，前端可据此做统一提示与处理。
package errs

import (
	"errors"
	"fmt"
	"net/http"
)

// 通用业务错误码。
const (
	CodeOK              = 0
	CodeBadRequest      = 40000
	CodeUnauthorized    = 40100
	CodeTokenExpired    = 40101
	CodeTokenInvalid    = 40102
	CodeAccountLocked   = 40103
	CodeAPIKeyInvalid   = 40107
	CodeForbidden       = 40300
	CodeNotFound        = 40400
	CodeMethodNotAllow  = 40500
	CodeConflict        = 40900
	CodeValidation      = 40001
	CodeTooManyRequests = 42900
	CodeInternal        = 50000
	CodeUnavailable     = 50300
	CodeNotImplemented  = 50100
)

// Error 是贯穿业务层的错误类型，携带 HTTP 状态、业务码、用户可见文案与内部原因。
type Error struct {
	Status  int    `json:"-"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	Err     error  `json:"-"`
}

// Error 实现 error 接口，输出内部原因便于日志排查。
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap 支持 errors.Is / errors.As。
func (e *Error) Unwrap() error { return e.Err }

// WithDetails 附加字段级错误明细。
func (e *Error) WithDetails(d any) *Error {
	e.Details = d
	return e
}

// WithCause 附加内部原因。
func (e *Error) WithCause(err error) *Error {
	e.Err = err
	return e
}

// New 构造业务错误。
func New(status, code int, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

// Newf 构造带格式化文案的业务错误。
func Newf(status, code int, format string, args ...any) *Error {
	return New(status, code, fmt.Sprintf(format, args...))
}

// 常用构造器，覆盖 90% 的业务场景。
func BadRequest(msg string) *Error   { return New(http.StatusBadRequest, CodeBadRequest, msg) }
func Unauthorized(msg string) *Error { return New(http.StatusUnauthorized, CodeUnauthorized, msg) }

// APIKeyInvalid 凭证缺失、无效、停用或过期。与登录态失效分开编码，
// 方便调用方区分是「密钥问题」还是「用户会话问题」。
func APIKeyInvalid(msg string) *Error {
	return New(http.StatusUnauthorized, CodeAPIKeyInvalid, msg)
}
func Forbidden(msg string) *Error   { return New(http.StatusForbidden, CodeForbidden, msg) }
func NotFound(msg string) *Error    { return New(http.StatusNotFound, CodeNotFound, msg) }
func Conflict(msg string) *Error    { return New(http.StatusConflict, CodeConflict, msg) }
func Internal(msg string) *Error    { return New(http.StatusInternalServerError, CodeInternal, msg) }
func TooMany(msg string) *Error     { return New(http.StatusTooManyRequests, CodeTooManyRequests, msg) }
func Unavailable(msg string) *Error { return New(http.StatusServiceUnavailable, CodeUnavailable, msg) }
func Validation(msg string) *Error  { return New(http.StatusBadRequest, CodeValidation, msg) }

// Wrap 包装底层错误并对外隐藏细节。
func Wrap(status, code int, msg string, err error) *Error {
	return &Error{Status: status, Code: code, Message: msg, Err: err}
}

// From 将任意错误归一化为 *Error，未知错误统一降级为 500。
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var ve interface{ FieldErrors() map[string]string }
	if errors.As(err, &ve) {
		return New(http.StatusBadRequest, CodeValidation, "参数校验失败").WithDetails(ve.FieldErrors())
	}
	return Wrap(http.StatusInternalServerError, CodeInternal, "服务内部错误", err)
}

// Is 判断错误是否匹配指定业务码。
func Is(err error, code int) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}
