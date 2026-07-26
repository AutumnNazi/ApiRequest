package script

import (
	"strings"
	"testing"
	"time"

	"apirequest/backend/model"
)

func newTestSandbox() *Sandbox {
	return NewSandbox(2*time.Second,
		map[string]string{"merged": "m", "env_k": "env_v"},
		map[string]string{"env_k": "env_v"},
		map[string]string{"col_k": "col_v"},
		map[string]string{"g_k": "g_v"})
}

func TestVariableGetSet(t *testing.T) {
	s := newTestSandbox()
	err := s.Run(`
		if (pm.environment.get('env_k') !== 'env_v') throw new Error('env get');
		if (pm.collectionVariables.get('col_k') !== 'col_v') throw new Error('col get');
		if (pm.globals.get('g_k') !== 'g_v') throw new Error('global get');
		if (pm.variables.get('merged') !== 'm') throw new Error('merged get');
		pm.environment.set('token', 'abc');
		if (pm.environment.get('token') !== 'abc') throw new Error('set then get');
		if (pm.variables.get('token') !== 'abc') throw new Error('set visible in merged');
		pm.environment.unset('env_k');
		if (pm.environment.has('env_k')) throw new Error('unset');
	`, "pre")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := s.Result()
	if r.EnvChanges.Set["token"] != "abc" || !r.EnvChanges.Unset["env_k"] {
		t.Errorf("env changes = %+v", r.EnvChanges)
	}
	if !r.CollectionChanges.Empty() || !r.GlobalChanges.Empty() {
		t.Error("unexpected col/global changes")
	}
}

func TestReplaceIn(t *testing.T) {
	s := newTestSandbox()
	err := s.Run(`
		var out = pm.variables.replaceIn('x-{{merged}}-y');
		if (out !== 'x-m-y') throw new Error('replaceIn got ' + out);
	`, "pre")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestPreRequestMutation(t *testing.T) {
	s := newTestSandbox()
	req := &model.HttpRequest{Method: "GET", Url: "https://a.io", Headers: []model.KV{}}
	s.SetRequest(req)
	err := s.Run(`
		pm.request.headers.add({key: 'X-Trace', value: 't1'});
		pm.request.headers.upsert({key: 'x-trace', value: 't2'}); // 大小写不敏感更新
		pm.request.method = 'POST';
		pm.request.url = 'https://b.io';
	`, "pre")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if req.Method != "POST" || req.Url != "https://b.io" {
		t.Errorf("mutated = %s %s", req.Method, req.Url)
	}
	if len(req.Headers) != 1 || req.Headers[0].Value != "t2" {
		t.Errorf("headers = %+v", req.Headers)
	}
}

func TestTestsAndExpect(t *testing.T) {
	s := newTestSandbox()
	s.SetResponse(&model.ResponseResult{
		Status: 200, StatusText: "OK",
		Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}},
		Body:    model.ResponseBody{Inline: true, Text: `{"code":0,"items":[1,2,3]}`},
		Timing:  model.Timing{TotalMs: 12},
	})
	err := s.Run(`
		pm.test('status is 200', function () {
			pm.expect(pm.response.code).to.equal(200);
		});
		pm.test('json body', function () {
			var j = pm.response.json();
			pm.expect(j.code).to.equal(0);
			pm.expect(j.items).to.have.lengthOf(3);
			pm.expect(j.items).to.include(2);
			pm.expect(j).to.have.property('code', 0);
		});
		pm.test('header check', function () {
			pm.expect(pm.response.headers.get('content-type')).to.include('json');
		});
		pm.test('this one fails', function () {
			pm.expect(pm.response.code).to.equal(404);
		});
		pm.test('negation', function () {
			pm.expect(pm.response.code).to.not.equal(500);
		});
	`, "test")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := s.Result()
	if len(r.TestResults) != 5 {
		t.Fatalf("tests = %d, want 5", len(r.TestResults))
	}
	wantPass := []bool{true, true, true, false, true}
	for i, tr := range r.TestResults {
		if tr.Pass != wantPass[i] {
			t.Errorf("test %q pass = %v, want %v (err %s)", tr.Name, tr.Pass, wantPass[i], tr.Error)
		}
	}
	if !strings.Contains(r.TestResults[3].Error, "expected") {
		t.Errorf("fail message = %q", r.TestResults[3].Error)
	}
}

func TestConsoleCapture(t *testing.T) {
	s := newTestSandbox()
	err := s.Run(`console.log('hello', 42); console.warn('careful');`, "pre")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	r := s.Result()
	if len(r.Logs) != 2 || r.Logs[0] != "hello 42" || r.Logs[1] != "[warn] careful" {
		t.Errorf("logs = %v", r.Logs)
	}
}

func TestTimeout(t *testing.T) {
	s := NewSandbox(100*time.Millisecond, nil, nil, nil, nil)
	err := s.Run(`while (true) {}`, "pre")
	if err == nil {
		t.Fatal("want timeout error")
	}
	ae, ok := err.(*model.AppError)
	if !ok || ae.Kind != model.KindScript || ae.Detail != "script timeout" {
		t.Errorf("err = %v", err)
	}
}

func TestScriptErrorHasPhase(t *testing.T) {
	s := newTestSandbox()
	err := s.Run(`throw new Error('boom')`, "test")
	ae, ok := err.(*model.AppError)
	if !ok || ae.Phase != "test" || !strings.Contains(ae.Detail, "boom") {
		t.Errorf("err = %v", err)
	}
}

func TestNoHostAccess(t *testing.T) {
	s := newTestSandbox()
	// require / fetch / process 都不应存在
	err := s.Run(`
		if (typeof require !== 'undefined') throw new Error('require leaked');
		if (typeof fetch !== 'undefined') throw new Error('fetch leaked');
		if (typeof process !== 'undefined') throw new Error('process leaked');
	`, "pre")
	if err != nil {
		t.Fatalf("sandbox leaked: %v", err)
	}
}
