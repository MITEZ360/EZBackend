package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MITEZ360/tenon/pkg/gov"
)

// rawCall 发送原始请求体，用于构造畸形 JSON 这类 jsonCall 造不出来的请求。
func (e *env) rawCall(method, path, body string, headers map[string]string) (int, map[string]any) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.server.URL+path, strings.NewReader(body))
	if err != nil {
		e.t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// TestGovernanceParamErrors 错误参数必须得到明确的 4xx 与业务错误码，
// 不能是 500，也不能"接受了但存了脏数据"。
func TestGovernanceParamErrors(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()
	internal := internalHeaders()

	// 先建一个服务，后面的用例才有对照物。
	if code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/services",
		map[string]any{"name": "edge-svc"}, auth); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("准备服务失败: %d %v", code, body)
	}

	cases := []struct {
		name    string
		method  string
		path    string
		body    string
		headers map[string]string
		want    int
	}{
		{"无令牌读服务目录", http.MethodGet, "/api/v1/gov/services", "", nil, http.StatusUnauthorized},
		{"错误令牌读服务目录", http.MethodGet, "/api/v1/gov/services", "", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"畸形 JSON", http.MethodPost, "/api/v1/gov/services", `{"name":`, auth, http.StatusBadRequest},
		{"空请求体", http.MethodPost, "/api/v1/gov/services", "", auth, http.StatusBadRequest},
		{"数组代替对象", http.MethodPost, "/api/v1/gov/services", `[{"name":"x"}]`, auth, http.StatusBadRequest},
		{"字段类型错误", http.MethodPost, "/api/v1/gov/services", `{"name":123}`, auth, http.StatusBadRequest},
		{"服务名为空", http.MethodPost, "/api/v1/gov/services", `{"name":"   "}`, auth, http.StatusBadRequest},
		{"服务名含空格", http.MethodPost, "/api/v1/gov/services", `{"name":"bad name"}`, auth, http.StatusBadRequest},
		{"服务名含斜杠", http.MethodPost, "/api/v1/gov/services", `{"name":"a/b"}`, auth, http.StatusBadRequest},
		{"服务名过长", http.MethodPost, "/api/v1/gov/services", `{"name":"` + strings.Repeat("x", 100) + `"}`, auth, http.StatusBadRequest},
		{"注册缺令牌", http.MethodPost, "/api/v1/gov/instances/register", `{"service":"edge-svc","host":"127.0.0.1","port":8080}`, nil, http.StatusUnauthorized},
		{"注册端口超界", http.MethodPost, "/api/v1/gov/instances/register", `{"service":"edge-svc","host":"127.0.0.1","port":70000}`, internal, http.StatusBadRequest},
		{"注册端口为负", http.MethodPost, "/api/v1/gov/instances/register", `{"service":"edge-svc","host":"127.0.0.1","port":-1}`, internal, http.StatusBadRequest},
		{"注册主机含空格", http.MethodPost, "/api/v1/gov/instances/register", `{"service":"edge-svc","host":"1.2.3.4 5","port":80}`, internal, http.StatusBadRequest},
		{"注册主机过长", http.MethodPost, "/api/v1/gov/instances/register", `{"service":"edge-svc","host":"` + strings.Repeat("h", 300) + `","port":80}`, internal, http.StatusBadRequest},
		{"注册服务名非法", http.MethodPost, "/api/v1/gov/instances/register", `{"service":"bad svc","host":"1.2.3.4","port":80}`, internal, http.StatusBadRequest},
		{"路由目标不存在", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api/x","service":"ghost"}`, auth, http.StatusBadRequest},
		{"路由方法非法", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api/x","service":"edge-svc","methods":["TRACE"]}`, auth, http.StatusBadRequest},
		{"路由前缀含空格", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api x","service":"edge-svc"}`, auth, http.StatusBadRequest},
		{"路由前缀含查询串", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api?x=1","service":"edge-svc"}`, auth, http.StatusBadRequest},
		{"路由超时越界", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api/t","service":"edge-svc","timeoutMs":-1}`, auth, http.StatusBadRequest},
		{"路由重试越界", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api/r","service":"edge-svc","retries":9}`, auth, http.StatusBadRequest},
		{"路由权重为负", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api/w","service":"edge-svc","weight":-1}`, auth, http.StatusBadRequest},
		{"路由状态直接发布", http.MethodPost, "/api/v1/gov/routes", `{"prefix":"/api/s","service":"edge-svc","status":"published"}`, auth, http.StatusBadRequest},
		{"畸形 JSON 路由", http.MethodPost, "/api/v1/gov/routes", `{"prefix":}`, auth, http.StatusBadRequest},
		{"空编译请求", http.MethodPost, "/api/v1/gov/routes/compile", `[]`, auth, http.StatusOK},
	}
	for _, tc := range cases {
		code, body := e.rawCall(tc.method, tc.path, tc.body, tc.headers)
		if code != tc.want {
			t.Fatalf("%s: 期望 %d，实际 %d %v", tc.name, tc.want, code, body)
		}
		// 业务错误必须带业务码，前端要靠它区分"参数问题"与"服务故障"。
		if code == http.StatusBadRequest {
			if c, _ := body["code"].(float64); c == 0 {
				t.Fatalf("%s: 400 响应缺少业务码: %v", tc.name, body)
			}
		}
	}

	// 错误请求不应留下任何脏数据。
	var list []any
	code, body := e.jsonCall(http.MethodGet, "/api/v1/gov/routes", nil, auth)
	if code != http.StatusOK {
		t.Fatalf("读取路由失败: %d %v", code, body)
	}
	list, _ = dataOf(body)["list"].([]any)
	if len(list) != 0 {
		t.Fatalf("错误参数不应写入路由，实际 %d 条", len(list))
	}
}

// TestGovernanceInjectionStrings 注入式字符串只能当普通字面量处理。
func TestGovernanceInjectionStrings(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()

	// 服务名走标识符校验，注入串直接 400。
	code, _ := e.rawCall(http.MethodPost, "/api/v1/gov/services",
		`{"name":"x'; DROP TABLE gov_service;--"}`, auth)
	if code != http.StatusBadRequest {
		t.Fatalf("注入式服务名应被拒绝，实际 %d", code)
	}

	// 查询参数里的注入串：按等值匹配，返回空列表而不是全表。
	for _, q := range []string{
		"/api/v1/gov/instances?service=%27%20OR%201%3D1--",
		"/api/v1/gov/instances?status=%27%3B%20DELETE%20FROM%20gov_instance%3B--",
	} {
		code, body := e.jsonCall(http.MethodGet, q, nil, auth)
		if code != http.StatusOK {
			t.Fatalf("%s 应正常返回空列表，实际 %d %v", q, code, body)
		}
		list, _ := dataOf(body)["list"].([]any)
		if len(list) != 0 {
			t.Fatalf("%s 不应匹配到任何实例，实际 %d 条", q, len(list))
		}
	}

	// 表还在，接口还能正常写。
	if code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/services",
		map[string]any{"name": "after-injection"}, auth); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("注入尝试后建服务失败: %d %v", code, body)
	}
}

// TestGovernanceUnicodeAndLongText 中文描述要能存，标识符类字段要按规则拒绝。
func TestGovernanceUnicodeAndLongText(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()

	desc := strings.Repeat("治理面中文描述。", 200)
	code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/services", map[string]any{
		"name": "unicode-svc", "owner": "运维组", "desc": desc, "group": "订单",
	}, auth)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("中文描述应可保存: %d %v", code, body)
	}
	svc := dataOf(body)
	if got, _ := svc["desc"].(string); got != desc {
		t.Fatalf("描述被截断或改写: 长度 %d != %d", len(got), len(desc))
	}

	// 中文服务名不是合法标识符：它会被当成注册键与路由目标。
	code, body = e.jsonCall(http.MethodPost, "/api/v1/gov/services", map[string]any{"name": "订单服务"}, auth)
	if code != http.StatusBadRequest {
		t.Fatalf("中文服务名应被拒绝，实际 %d %v", code, body)
	}
}

// TestOversizedBodyRejected 超过 server.max_body_size 的请求必须得到 413，
// 而不是被当成"JSON 写错了"。
func TestOversizedBodyRejected(t *testing.T) {
	e := newEnvWithServer(t, "  max_body_size: 512")
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()

	big := strings.Repeat("a", 4096)
	code, body := e.rawCall(http.MethodPost, "/api/v1/gov/services",
		`{"name":"big-svc","desc":"`+big+`"}`, auth)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("超大请求体应返回 413，实际 %d %v", code, body)
	}
	if c, _ := body["code"].(float64); c != 41300 {
		t.Fatalf("业务码应为 41300，实际 %v", body["code"])
	}

	// 限制之内的请求照常工作。
	if code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/services",
		map[string]any{"name": "small-svc", "desc": "ok"}, auth); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("正常请求不应受影响: %d %v", code, body)
	}
}

// TestGovernanceConcurrentHTTP 并发压控制面：所有请求都必须得到明确结果，
// 不能出现 5xx，也不能出现"同一实例两条记录"。
func TestGovernanceConcurrentHTTP(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()
	internal := internalHeaders()

	// 准备服务与一条路由。
	if code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/services",
		map[string]any{"name": "load-svc"}, auth); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("建服务失败: %d %v", code, body)
	}
	if code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/routes",
		map[string]any{"prefix": "/api/load", "service": "load-svc", "weight": 100}, auth); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("建路由失败: %d %v", code, body)
	}

	var badStatus atomic.Int64
	var serverErr atomic.Int64

	// 1) 40 个并发拉快照，期间穿插发布。
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				code, _ := e.callRaw(http.MethodGet, "/internal/gov/snapshot", nil, internal)
				if code >= 500 {
					serverErr.Add(1)
				} else if code != http.StatusOK && code != http.StatusNotModified {
					badStatus.Add(1)
				}
			}
		}()
	}
	for i := 0; i < 6; i++ {
		if code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/routes/publish",
			map[string]any{"note": fmt.Sprintf("压测 %d", i)}, auth); code != http.StatusOK {
			t.Errorf("并发发布失败: %d %v", code, body)
		}
	}
	close(stop)
	wg.Wait()
	if serverErr.Load() != 0 {
		t.Fatalf("快照接口出现 %d 次 5xx", serverErr.Load())
	}
	if badStatus.Load() != 0 {
		t.Fatalf("快照接口出现 %d 次非预期状态", badStatus.Load())
	}

	// 2) 30 个并发注册同一个实例：必须幂等。
	var regErr atomic.Int64
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/instances/register", map[string]any{
				"service": "load-svc", "instanceId": "same-inst", "host": "10.1.1.1", "port": 8080,
			}, internal)
			if code != http.StatusOK {
				regErr.Add(1)
				t.Logf("并发注册失败: %d %v", code, body)
			}
		}()
	}
	wg.Wait()
	if regErr.Load() != 0 {
		t.Fatalf("并发注册出现 %d 次失败", regErr.Load())
	}
	code, body := e.jsonCall(http.MethodGet, "/api/v1/gov/instances?service=load-svc", nil, auth)
	if code != http.StatusOK {
		t.Fatalf("读实例失败: %d %v", code, body)
	}
	list, _ := dataOf(body)["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("同一实例并发注册应只有 1 条，实际 %d", len(list))
	}

	// 3) 并发发布产生的版本号必须互不相同。
	code, body = e.jsonCall(http.MethodGet, "/api/v1/gov/releases", nil, auth)
	if code != http.StatusOK {
		t.Fatalf("读发布历史失败: %d %v", code, body)
	}
	rels, _ := dataOf(body)["list"].([]any)
	seen := map[float64]bool{}
	active := 0
	for _, r := range rels {
		rel := r.(map[string]any)
		rev, _ := rel["revision"].(float64)
		if seen[rev] {
			t.Fatalf("发布版本号重复: %v", rev)
		}
		seen[rev] = true
		if rel["status"] == "active" {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("同一域应只有 1 个 active 发布，实际 %d（共 %d 条）", active, len(rels))
	}
}

// TestSnapshotFingerprintContract 内容指纹协议：同内容 304、不同内容 200、
// 乱填不报错。
func TestSnapshotFingerprintContract(t *testing.T) {
	e := newEnv(t)
	internal := internalHeaders()

	code, raw := e.callRaw(http.MethodGet, "/internal/gov/snapshot", nil, internal)
	if code != http.StatusOK {
		t.Fatalf("首次拉取失败: %d %s", code, raw)
	}
	var snap gov.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("快照无法解码: %v", err)
	}
	fp := snap.Fingerprint()

	if code, _ := e.callRaw(http.MethodGet, "/internal/gov/snapshot?fp="+fp, nil, internal); code != http.StatusNotModified {
		t.Fatalf("相同指纹应 304，实际 %d", code)
	}
	for _, bad := range []string{"deadbeef", "", strings.Repeat("f", 128), "%20"} {
		if code, _ := e.callRaw(http.MethodGet, "/internal/gov/snapshot?fp="+bad, nil, internal); code != http.StatusOK {
			t.Fatalf("指纹 %q 应返回 200，实际 %d", bad, code)
		}
	}

	// 实例状态变化会改变内容 → 指纹变化 → 不能再 304。
	e.mustLogin("admin", "Admin@123456")
	if code, body := e.jsonCall(http.MethodPost, "/api/v1/gov/services",
		map[string]any{"name": "fp-svc"}, e.authHeaders()); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("建服务失败: %d %v", code, body)
	}
	code, raw2 := e.callRaw(http.MethodGet, "/internal/gov/snapshot?fp="+fp, nil, internal)
	if code != http.StatusOK {
		t.Fatalf("内容变化后不应再 304，实际 %d", code)
	}
	var snap2 gov.Snapshot
	_ = json.Unmarshal(raw2, &snap2)
	if snap2.Fingerprint() == fp {
		t.Fatal("服务目录变化后指纹应改变")
	}
}

// TestUnknownGovernancePaths 未注册的治理路径必须 404，不能落到前端单页返回 200。
func TestUnknownGovernancePaths(t *testing.T) {
	e := newEnv(t)
	e.mustLogin("admin", "Admin@123456")
	auth := e.authHeaders()

	for _, path := range []string{
		"/api/v1/gov/nope",
		"/api/v1/gov/services/../../etc/passwd",
		"/api/v1/gov/routes/not-a-uuid",
	} {
		code, _ := e.callRaw(http.MethodGet, path, nil, auth)
		if code == http.StatusOK {
			t.Fatalf("%s 不应返回 200", path)
		}
	}
	// 已注册路径上的不支持方法也必须是 4xx。
	code, _ := e.callRaw(http.MethodPatch, "/api/v1/gov/services", []byte(`{}`), auth)
	if code < 400 || code >= 500 {
		t.Fatalf("不支持的方法应返回 4xx，实际 %d", code)
	}
}

// TestGatewayExtremeUpstream 上游返回 5xx、超时、连接拒绝时网关的映射。
//
// 5xx 是上游的真实响应，网关应原样透传（不是自己造 503）；连接层失败
// 才由网关兜底成 503。
func TestGatewayExtremeUpstream(t *testing.T) {
	cp := newEnv(t)
	cp.mustLogin("admin", "Admin@123456")
	auth := cp.authHeaders()

	// 上游：/boom 返回 500，/slow 睡 300ms，其余正常。
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/boom":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "upstream error")
		case "/slow":
			time.Sleep(300 * time.Millisecond)
			_, _ = io.WriteString(w, "slow")
		default:
			hits.Add(1)
			_, _ = io.WriteString(w, "ok")
		}
	}))
	t.Cleanup(up.Close)

	host, port := splitAddr(t, up.URL)
	gw := startGateway(t, cp.server.URL)
	client := &http.Client{Timeout: 3 * time.Second}

	// 建服务 + 实例 + 路由（短超时、带重试）。
	if code, body := cp.jsonCall(http.MethodPost, "/api/v1/gov/services",
		map[string]any{"name": "ext-svc"}, auth); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("建服务失败: %d %v", code, body)
	}
	if code, body := cp.jsonCall(http.MethodPost, "/api/v1/gov/instances/register", map[string]any{
		"service": "ext-svc", "instanceId": "ext-1", "host": host, "port": port,
	}, internalHeaders()); code != http.StatusOK {
		t.Fatalf("注册实例失败: %d %v", code, body)
	}
	if code, body := cp.jsonCall(http.MethodPost, "/api/v1/gov/routes", map[string]any{
		"prefix": "/api/ext", "service": "ext-svc", "strip": true,
		"timeoutMs": 100, "retries": 2,
	}, auth); code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("建路由失败: %d %v", code, body)
	}
	if code, body := cp.jsonCall(http.MethodPost, "/api/v1/gov/routes/publish", map[string]any{}, auth); code != http.StatusOK {
		t.Fatalf("发布失败: %d %v", code, body)
	}
	// 等网关拿到快照并开始转发。
	waitFor(t, 5*time.Second, "网关跟随快照", func() bool {
		return statusOf(t, client, gw+"/api/ext/ping") == http.StatusOK
	})

	// 上游 500 应原样透传。
	if code := statusOf(t, client, gw+"/api/ext/boom"); code != http.StatusInternalServerError {
		t.Fatalf("上游 500 应透传，实际 %d", code)
	}
	// 上游超时应由网关兜底成 503，而不是挂住或返回 500。
	if code := statusOf(t, client, gw+"/api/ext/slow"); code != http.StatusServiceUnavailable {
		t.Fatalf("上游超时应返回 503，实际 %d", code)
	}
	// 正常路径仍可用。
	if code := statusOf(t, client, gw+"/api/ext/ping"); code != http.StatusOK {
		t.Fatalf("正常路径应 200，实际 %d", code)
	}
}
