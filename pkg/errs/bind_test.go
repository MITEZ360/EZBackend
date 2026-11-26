package errs

import (
	"fmt"
	"net/http"
	"testing"
)

// TestBindErrorMapsBodyTooLarge 请求体超限必须映射成 413，
// 否则调用方会把它当成 JSON 写错，反复微调也不会有任何效果。
func TestBindErrorMapsBodyTooLarge(t *testing.T) {
	raw := &http.MaxBytesError{Limit: 512}
	cases := []error{
		raw,
		fmt.Errorf("解析请求失败: %w", raw), // 被包裹一层也要能识别
	}
	for _, err := range cases {
		e := BindError(err)
		if e.Status != http.StatusRequestEntityTooLarge || e.Code != CodePayloadTooLarge {
			t.Fatalf("应为 413/%d，实际 %d/%d（%v）", CodePayloadTooLarge, e.Status, e.Code, err)
		}
	}
}

// TestBindErrorFallsBackToValidation 其余绑定错误保持 400/40001，
// 不能因为新增分支改变既有语义。
func TestBindErrorFallsBackToValidation(t *testing.T) {
	e := BindError(fmt.Errorf("unexpected EOF"))
	if e.Status != http.StatusBadRequest || e.Code != CodeValidation {
		t.Fatalf("应为 400/%d，实际 %d/%d", CodeValidation, e.Status, e.Code)
	}
	if e.Err == nil {
		t.Fatal("应保留原始错误，便于日志排查")
	}
}
