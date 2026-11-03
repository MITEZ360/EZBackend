package utils

import (
	"strings"
	"testing"
	"time"
)

func TestMask(t *testing.T) {
	if got := MaskPhone("13712345678"); got != "137****5678" {
		t.Fatalf("手机号脱敏错误: %s", got)
	}
	if got := MaskPhone("123"); got != "***" {
		t.Fatalf("短号码应整体脱敏: %s", got)
	}
	if got := MaskEmail("alice@example.com"); got != "al***@example.com" {
		t.Fatalf("邮箱脱敏错误: %s", got)
	}
	if got := MaskEmail("invalid"); got != "***" {
		t.Fatalf("非法邮箱应整体脱敏: %s", got)
	}
	if got := MaskSecret("1234567890abcdef"); got == "1234567890abcdef" {
		t.Fatal("密钥应被脱敏")
	}
}

func TestBuildTree(t *testing.T) {
	type node struct {
		ID       string
		ParentID string
		Name     string
		Children []*node
	}
	rows := []*node{
		{ID: "1", ParentID: "", Name: "root1"},
		{ID: "2", ParentID: "1", Name: "child1"},
		{ID: "3", ParentID: "1", Name: "child2"},
		{ID: "4", ParentID: "", Name: "root2"},
		{ID: "5", ParentID: "404", Name: "orphan"},
	}
	tree := BuildTree(rows, func(n *node) string { return n.ID },
		func(n *node) string { return n.ParentID },
		func(n *node, children []*node) { n.Children = children },
		func(id string) bool { return id == "" })
	if len(tree) != 2 {
		t.Fatalf("根节点数应为 2，实际 %d", len(tree))
	}
	if len(tree[0].Children) != 2 {
		t.Fatalf("root1 子节点应为 2，实际 %d", len(tree[0].Children))
	}
}

func TestRandomString(t *testing.T) {
	a := RandomString(16)
	b := RandomString(16)
	if len(a) != 16 || a == b {
		t.Fatalf("随机字符串异常: %q %q", a, b)
	}
	if strings.ContainsAny(a, " ") {
		t.Fatal("随机字符串不应包含空格")
	}
	if len(RandomToken(16)) < 16 {
		t.Fatal("令牌长度不足")
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 512: "512 B", 2048: "2.0KB", 5 * 1024 * 1024: "5.0MB"}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Fatalf("FormatBytes(%d) = %s，期望 %s", in, got, want)
		}
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "passwd",
		"a/b/c.txt":        "c.txt",
		`..\..\win.ini`:    "win.ini",
		"normal.pdf":       "normal.pdf",
	}
	for in, want := range cases {
		got := SafeFilename(in)
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
			t.Fatalf("文件名未净化: %q -> %q", in, got)
		}
		if want != "" && got != want {
			t.Fatalf("SafeFilename(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestUniqueStrings(t *testing.T) {
	got := UniqueStrings([]string{"a", "b", "a", "", "c", "b"})
	if len(got) != 3 {
		t.Fatalf("去重结果错误: %v", got)
	}
}

func TestTodayRange(t *testing.T) {
	start, end := TodayRange(time.Now())
	if !start.Before(end) || end.Sub(start) != 24*time.Hour {
		t.Fatalf("今日区间异常: %v ~ %v", start, end)
	}
	if start.Hour() != 0 || start.Minute() != 0 {
		t.Fatalf("起始时间应为零点: %v", start)
	}
	if DaysAgo(time.Now(), 7).After(time.Now()) {
		t.Fatal("DaysAgo 结果异常")
	}
}

func TestInt8Or(t *testing.T) {
	if got := Int8Or(nil, 1); got != 1 {
		t.Fatalf("nil 应返回默认值，实际 %d", got)
	}
	zero := int8(0)
	if got := Int8Or(&zero, 1); got != 0 {
		t.Fatalf("显式传 0 不应被默认值覆盖，实际 %d", got)
	}
	one := int8(1)
	if got := Int8Or(&one, 0); got != 1 {
		t.Fatalf("显式传 1 应原样返回，实际 %d", got)
	}
}
