package user

import (
	"strings"
	"testing"
)

func TestReleaseUsername(t *testing.T) {
	if got := releaseUsername("zhangsan", "123"); got != "zhangsan#deleted#123" {
		t.Fatalf("占位用户名错误: %s", got)
	}
	// 超长用户名必须截断，且总长度不超过列宽 64
	long := strings.Repeat("a", 80)
	got := releaseUsername(long, "2117247533256744960")
	if len(got) > 64 {
		t.Fatalf("占位用户名超长: %d", len(got))
	}
	if !strings.HasSuffix(got, "#deleted#2117247533256744960") {
		t.Fatalf("占位用户名后缀丢失: %s", got)
	}
}
