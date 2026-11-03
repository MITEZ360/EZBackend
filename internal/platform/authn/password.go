package authn

import (
	"errors"
	"fmt"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// 密码错误。
var (
	ErrPasswordMismatch = errors.New("密码不正确")
	ErrPasswordTooWeak  = errors.New("密码强度不足")
)

// HashPassword 使用 bcrypt 生成散列（cost=10，兼顾安全与登录耗时）。
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", fmt.Errorf("密码加密失败: %w", err)
	}
	return string(b), nil
}

// CheckPassword 校验密码，使用 bcrypt 恒定时间比较。
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// PasswordPolicy 描述密码策略。
type PasswordPolicy struct {
	MinLen int
	Level  int // 1: 仅长度 2: 字母+数字 3: 大小写+数字+符号
}

// ValidatePassword 按策略校验密码强度。
func ValidatePassword(pw string, p PasswordPolicy) error {
	if p.MinLen <= 0 {
		p.MinLen = 8
	}
	if len([]rune(pw)) < p.MinLen {
		return fmt.Errorf("%w: 长度至少 %d 位", ErrPasswordTooWeak, p.MinLen)
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	kinds := 0
	for _, b := range []bool{hasUpper, hasLower, hasDigit, hasSpecial} {
		if b {
			kinds++
		}
	}
	switch p.Level {
	case 1:
		return nil
	case 2:
		if kinds < 2 || !hasDigit || !(hasLower || hasUpper) {
			return fmt.Errorf("%w: 需同时包含字母与数字", ErrPasswordTooWeak)
		}
	case 3:
		if kinds < 4 {
			return fmt.Errorf("%w: 需包含大写字母、小写字母、数字与特殊符号", ErrPasswordTooWeak)
		}
	}
	return nil
}
