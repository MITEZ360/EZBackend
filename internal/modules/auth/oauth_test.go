package auth

import (
	"encoding/json"
	"testing"
)

func TestLookupJSON(t *testing.T) {
	obj := map[string]any{
		"response": map[string]any{"user": map[string]any{"id": float64(8801), "login": "carol"}},
		"emails":   []any{map[string]any{"value": "a@x.com"}, map[string]any{"value": "b@x.com"}},
	}
	cases := []struct {
		path string
		want string
	}{
		{"response.user.login", "carol"},
		{"response.user.id", "8801"},
		{"emails.0.value", "a@x.com"},
		{"emails.1.value", "b@x.com"},
		{"emails.9.value", ""},
		{"missing.path", ""},
	}
	for _, c := range cases {
		if got := stringVal(lookupJSON(obj, c.path)); got != c.want {
			t.Errorf("lookupJSON(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestFirstValue(t *testing.T) {
	obj := map[string]any{
		"login":  "carol",
		"name":   "Carol",
		"nested": map[string]any{"id": float64(1)},
	}
	cases := []struct {
		paths string
		want  string
	}{
		{"login,name", "carol"},
		{"missing, name", "Carol"},      // 候选路径，跳过空值
		{" nickname , login ", "carol"}, // trim 空白
		{"nested.id", "1"},
		{"a,b,c", ""},
	}
	for _, c := range cases {
		if got := firstValue(obj, c.paths); got != c.want {
			t.Errorf("firstValue(%q) = %q, want %q", c.paths, got, c.want)
		}
	}
}

func TestStringVal(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"  hi  ", "hi"},
		{float64(42), "42"},
		{float64(3.5), "3.5"},
		{true, "true"},
		{json.Number("9007199254740993"), "9007199254740993"},
	}
	for _, c := range cases {
		if got := stringVal(c.in); got != c.want {
			t.Errorf("stringVal(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruthy(t *testing.T) {
	for _, v := range []any{true, "true", "TRUE", "1", "yes", "Yes"} {
		if !truthy(v) {
			t.Errorf("truthy(%v) should be true", v)
		}
	}
	for _, v := range []any{false, "false", "0", "no", "", nil} {
		if truthy(v) {
			t.Errorf("truthy(%v) should be false", v)
		}
	}
}

func TestSanitizeUsername(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Carol Chen", "CarolChen"},
		{"  ..bob--  ", "bob"},
		{"user@example.com", "userexample.com"},
		{"中文名字", "oauth_user"},
		{"___", "oauth_user"},
	}
	for _, c := range cases {
		if got := sanitizeUsername(c.in); got != c.want {
			t.Errorf("sanitizeUsername(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := sanitizeUsername("abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz")
	if len([]rune(long)) > 32 {
		t.Errorf("sanitizeUsername 未截断到 32 字符: %d", len([]rune(long)))
	}
}

func TestShorten(t *testing.T) {
	if got := shorten("hello", 3); got != "hel" {
		t.Errorf("shorten = %q", got)
	}
	if got := shorten("hi", 5); got != "hi" {
		t.Errorf("shorten = %q", got)
	}
}
