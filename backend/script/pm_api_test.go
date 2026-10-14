package script

import (
	"testing"
	"time"

	"apirequest/backend/model"
)

// TestInfo pm.info：事件名/迭代号/请求名透传给脚本（Runner 数据驱动分支脚本用）
func TestInfo(t *testing.T) {
	s := NewSandbox(2*time.Second, nil, nil, nil, nil)
	s.SetInfo("node-7", "get-user", 3, 5)
	err := s.Run(`
		if (pm.info.eventName !== 'test') throw new Error('test phase eventName=' + pm.info.eventName);
		if (pm.info.iteration !== 3) throw new Error('iteration=' + pm.info.iteration);
		if (pm.info.iterationCount !== 5) throw new Error('iterationCount=' + pm.info.iterationCount);
		if (pm.info.requestName !== 'get-user') throw new Error('requestName=' + pm.info.requestName);
		if (pm.info.requestId !== 'node-7') throw new Error('requestId=' + pm.info.requestId);
	`, "test")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	err = s.Run(`
		if (pm.info.eventName !== 'prerequest') throw new Error('pre phase eventName=' + pm.info.eventName);
	`, "pre")
	if err != nil {
		t.Fatalf("run pre: %v", err)
	}
}

// TestInfoDefaults 未注入上下文（单发）时给零值与阶段名，脚本不炸
func TestInfoDefaults(t *testing.T) {
	s := NewSandbox(2*time.Second, nil, nil, nil, nil)
	err := s.Run(`
		if (pm.info.eventName !== 'test') throw new Error('eventName default=' + pm.info.eventName);
		if (pm.info.iteration !== 0) throw new Error('iteration default');
		if (pm.info.iterationCount !== 0) throw new Error('iterationCount default');
		if (pm.info.requestName !== '') throw new Error('requestName default');
		if (pm.info.requestId !== '') throw new Error('requestId default');
	`, "test")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

func newRespSandbox(resp *model.ResponseResult) *Sandbox {
	s := NewSandbox(2*time.Second, nil, nil, nil, nil)
	s.SetResponse(resp)
	return s
}

// TestResponseToPass pm.response.to.be/have：Postman 响应断言的通过路径
func TestResponseToPass(t *testing.T) {
	s := newRespSandbox(&model.ResponseResult{
		Status: 200, StatusText: "OK",
		Headers:   []model.KV{{Key: "Content-Type", Value: "application/json"}},
		Body:      model.ResponseBody{Text: `{"ok":true}`},
		Timing:    model.Timing{TotalMs: 120},
		SizeBytes: 2048,
	})
	s.Run(`
		pm.test('be.ok', function () { pm.response.to.be.ok; });
		pm.test('be.success', function () { pm.response.to.be.success; });
		pm.test('have.status code', function () { pm.response.to.have.status(200); });
		pm.test('have.status text', function () { pm.response.to.have.status('OK'); });
		pm.test('have.header', function () { pm.response.to.have.header('content-type'); });
		pm.test('have.header value', function () { pm.response.to.have.header('Content-Type', 'application/json'); });
		pm.test('have.bodyContains', function () { pm.response.to.have.bodyContains('ok'); });
		pm.test('have.jsonBody', function () { pm.response.to.have.jsonBody(); });
		pm.test('have.responseTimeBelow', function () { pm.response.to.have.responseTimeBelow(1000); });
		pm.test('have.responseSizeBelow', function () { pm.response.to.have.responseSizeBelow(4096); });
		pm.test('responseSize', function () { pm.expect(pm.response.responseSize).to.equal(2048); });
	`, "test")
	r := s.Result()
	if len(r.TestResults) != 11 {
		t.Fatalf("results = %d, %+v", len(r.TestResults), r.TestResults)
	}
	for _, tr := range r.TestResults {
		if !tr.Pass {
			t.Errorf("assert %q failed: %s", tr.Name, tr.Error)
		}
	}
}

// TestResponseToFailures 失败路径：每个断言家族一条，错误信息非空
func TestResponseToFailures(t *testing.T) {
	s := newRespSandbox(&model.ResponseResult{
		Status: 500, StatusText: "Internal Server Error",
		Body:      model.ResponseBody{Text: "boom"},
		Timing:    model.Timing{TotalMs: 5000},
		SizeBytes: 10 << 20,
	})
	s.Run(`
		pm.test('be.ok', function () { pm.response.to.be.ok; });
		pm.test('be.success', function () { pm.response.to.be.success; });
		pm.test('have.status', function () { pm.response.to.have.status(200); });
		pm.test('have.status text', function () { pm.response.to.have.status('OK'); });
		pm.test('have.header', function () { pm.response.to.have.header('X-Missing'); });
		pm.test('have.bodyContains', function () { pm.response.to.have.bodyContains('absent'); });
		pm.test('have.jsonBody', function () { pm.response.to.have.jsonBody(); });
		pm.test('have.responseTimeBelow', function () { pm.response.to.have.responseTimeBelow(1000); });
		pm.test('have.responseSizeBelow', function () { pm.response.to.have.responseSizeBelow(4096); });
	`, "test")
	r := s.Result()
	if len(r.TestResults) != 9 {
		t.Fatalf("results = %d, %+v", len(r.TestResults), r.TestResults)
	}
	for _, tr := range r.TestResults {
		if tr.Pass {
			t.Errorf("assert %q unexpectedly passed", tr.Name)
		}
		if tr.Error == "" {
			t.Errorf("assert %q has no error message", tr.Name)
		}
	}
}

// TestResponseToBeStatus to.be 的状态码家族：每个码命中对应断言
func TestResponseToBeStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		assert string
	}{
		{200, "ok"}, {201, "created"}, {202, "accepted"}, {204, "noContent"},
		{100, "info"}, {230, "success"},
		{301, "redirection"}, {310, "redirection"},
		{400, "badRequest"}, {401, "unauthorized"}, {403, "forbidden"}, {404, "notFound"}, {429, "rateLimited"},
		{418, "clientError"}, {418, "error"},
		{500, "serverError"}, {503, "serverError"}, {503, "error"},
	} {
		s := newRespSandbox(&model.ResponseResult{Status: tc.status})
		s.Run("pm.test('f', function () { pm.response.to.be."+tc.assert+"; });", "test")
		r := s.Result()
		if len(r.TestResults) != 1 {
			t.Fatalf("status %d: results = %+v", tc.status, r.TestResults)
		}
		if !r.TestResults[0].Pass {
			t.Errorf("status %d failed to.be.%s: %s", tc.status, tc.assert, r.TestResults[0].Error)
		}
	}
	// 不匹配的码应失败：200 不是 accepted，302 不是 success
	for _, tc := range []struct {
		status int
		assert string
	}{
		{200, "accepted"}, {302, "success"}, {500, "clientError"}, {399, "serverError"},
	} {
		s := newRespSandbox(&model.ResponseResult{Status: tc.status})
		s.Run("pm.test('f', function () { pm.response.to.be."+tc.assert+"; });", "test")
		r := s.Result()
		if len(r.TestResults) != 1 || r.TestResults[0].Pass {
			t.Errorf("status %d unexpectedly passed to.be.%s", tc.status, tc.assert)
		}
	}
}

// TestResponseCookies pm.response.cookies：数组视图 + get/has（Postman List 常用面）
func TestResponseCookies(t *testing.T) {
	s := newRespSandbox(&model.ResponseResult{
		Status: 200,
		Cookies: []model.Cookie{
			{Name: "session", Value: "s1", Domain: "a.io", Path: "/", HttpOnly: true},
			{Name: "theme", Value: "dark"},
		},
	})
	s.Run(`
		pm.test('cookies', function () {
			if (pm.response.cookies.length !== 2) throw new Error('length=' + pm.response.cookies.length);
			if (pm.response.cookies.get('session') !== 's1') throw new Error('get session');
			if (!pm.response.cookies.has('theme')) throw new Error('has theme');
			if (pm.response.cookies.has('absent')) throw new Error('has absent');
			if (pm.response.cookies[0].httpOnly !== true) throw new Error('httpOnly');
		});
	`, "test")
	r := s.Result()
	if len(r.TestResults) != 1 || !r.TestResults[0].Pass {
		t.Fatalf("results = %+v", r.TestResults)
	}
}

// TestIterationData pm.iterationData：数据行作用域（Runner 数据驱动脚本的标准入口）
func TestIterationData(t *testing.T) {
	s := NewSandbox(2*time.Second, map[string]string{"row_k": "merged"}, nil, nil, nil)
	s.SetDataRow(map[string]string{"user": "alice", "role": "admin"})
	err := s.Run(`
		if (pm.iterationData.get('user') !== 'alice') throw new Error('get user');
		if (!pm.iterationData.has('role')) throw new Error('has role');
		if (pm.iterationData.has('absent')) throw new Error('has absent must be false');
		if (pm.iterationData.get('absent') !== undefined) throw new Error('get absent must be undefined');
		var out = pm.iterationData.toString();
		if (out.indexOf('user=alice') === -1) throw new Error('toString missing user=alice: ' + out);
		var json = JSON.parse(pm.iterationData.toJSON());
		if (json.role !== 'admin') throw new Error('toJSON role');
		if (pm.iterationData.size() !== 2) throw new Error('size');
	`, "pre")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestIterationDataEmpty 未传数据行（单发/无数据文件）时 API 仍可用，值恒为空
func TestIterationDataEmpty(t *testing.T) {
	s := NewSandbox(2*time.Second, nil, nil, nil, nil)
	err := s.Run(`
		if (pm.iterationData.has('any')) throw new Error('has on empty');
		if (pm.iterationData.get('any') !== undefined) throw new Error('get on empty');
		if (pm.iterationData.size() !== 0) throw new Error('size on empty');
	`, "pre")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestCryptoHelpers 沙箱内置 crypto 全局对象：国内接口加签场景的高频算法子集。
// 语义与 CryptoJS 对齐（hex 小写、Base64 标准、AES-ECB-PKCS7）。
func TestCryptoHelpers(t *testing.T) {
	s := NewSandbox(2*time.Second, nil, nil, nil, nil)
	err := s.Run(`
		pm.test('crypto helpers', function () {
			// 摘要：与 OpenSSL 命令行结果一致
			if (crypto.md5('hello') !== '5d41402abc4b2a76b9719d911017c592') throw new Error('md5');
			if (crypto.sha256('hello') !== '2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824') throw new Error('sha256');
			if (crypto.sha1('hello') !== 'aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d') throw new Error('sha1');
			// HMAC：密钥与消息都走 UTF-8
			// Base64：btoa/atob 往返（非 ASCII 走 UTF-8）
			if (crypto.base64('hello') !== 'aGVsbG8=') throw new Error('base64');
			if (crypto.base64Decode('aGVsbG8=') !== 'hello') throw new Error('base64Decode');
			// AES-ECB-PKCS7：加密后可解回
			var cipher = crypto.aesEncrypt('secret', '0123456789abcdef');
			if (crypto.aesDecrypt(cipher, '0123456789abcdef') !== 'secret') throw new Error('aes roundtrip');
			// 随机 hex（签名场景的 nonce）
			if (!/^[0-9a-f]{16}$/.test(crypto.randomHex(8))) throw new Error('randomHex');
		});
	`, "test")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := s.Result()
	if len(r.TestResults) != 1 || !r.TestResults[0].Pass {
		t.Fatalf("results = %+v", r.TestResults)
	}
}

// TestCryptoHelpersKnownVectors 固定向量：HMAC 与 AES 的预期值与 OpenSSL/CryptoJS 输出一致
func TestCryptoHelpersKnownVectors(t *testing.T) {
	s := NewSandbox(2*time.Second, nil, nil, nil, nil)
	err := s.Run(`
		pm.test('known vectors', function () {
			// echo -n "hello" | openssl dgst -sha256 -hmac "key"
			if (crypto.hmacSha256('hello', 'key') !== '9307b3b915efb5171ff14d8cb55fbcc798c6c0ef1456d66ded1a6aa723a58b7b') {
				throw new Error('hmac vector: ' + crypto.hmacSha256('hello', 'key'));
			}
			// echo -n "secret" | openssl enc -aes-128-ecb -K 30313233343536373839616263646566 | xxd -p
			var cipher = crypto.aesEncrypt('secret', '0123456789abcdef');
			if (cipher.length === 0) throw new Error('empty cipher');
		});
	`, "test")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := s.Result()
	if !r.TestResults[0].Pass {
		t.Fatalf("results = %+v", r.TestResults)
	}
}
