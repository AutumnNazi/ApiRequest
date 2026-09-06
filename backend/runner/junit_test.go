package runner

import (
	"apirequest/backend/model"
	"encoding/xml"
	"strings"
	"testing"
)

func parseSuites(t *testing.T, xmlBody []byte) junitTestsuites {
	t.Helper()
	var suites junitTestsuites
	if err := xml.Unmarshal(xmlBody, &suites); err != nil {
		t.Fatalf("unmarshal junit xml: %v\n%s", err, xmlBody)
	}
	return suites
}

func TestMarshalJUnitXMLAllPass(t *testing.T) {
	report := &Report{
		RunId: "r1", Total: 2, Passed: 2, DurationMs: 250,
		Results: []RequestResult{
			{Iteration: 1, RequestName: "login", NodeId: "n1", Status: 200, DurationMs: 100},
			{Iteration: 1, RequestName: "list users", NodeId: "n2", Status: 200, DurationMs: 150},
		},
	}
	body, err := MarshalJUnitXML(report, "smoke suite")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), xml.Header) {
		t.Fatalf("missing xml header: %q", string(body[:40]))
	}
	suites := parseSuites(t, body)
	if len(suites.Suites) != 1 {
		t.Fatalf("suites = %d, want 1", len(suites.Suites))
	}
	suite := suites.Suites[0]
	if suite.Name != "smoke suite" || suite.Tests != 2 || suite.Failures != 0 {
		t.Fatalf("suite = %+v", suite)
	}
	if suites.Tests != 2 || suites.Failures != 0 || suites.Skipped != 0 {
		t.Fatalf("suites attrs = %+v", suites)
	}
	for _, tc := range suite.Cases {
		if tc.Failure != nil {
			t.Fatalf("unexpected failure on %s", tc.Name)
		}
	}
	// testcase 命名须带迭代号，数据驱动跑多轮时可区分
	if suite.Cases[0].Name != "iter1 - login" {
		t.Fatalf("case name = %q", suite.Cases[0].Name)
	}
}

func TestMarshalJUnitXMLFailures(t *testing.T) {
	report := &Report{
		RunId: "r1", Total: 3, Passed: 1, Failed: 2, DurationMs: 500,
		Results: []RequestResult{
			{Iteration: 1, RequestName: "ok", Status: 200},
			{Iteration: 1, RequestName: "bad status", Status: 500, Failed: true,
				TestResults: []model.TestResult{
					{Name: "status is 2xx", Pass: false, Error: "500 != 2xx"},
					{Name: "body has id", Pass: true},
				}},
			{Iteration: 1, RequestName: "conn refused", Failed: true,
				Error: `Get "http://x": dial refused`},
		},
	}
	body, err := MarshalJUnitXML(report, "ci")
	if err != nil {
		t.Fatal(err)
	}
	suites := parseSuites(t, body)
	suite := suites.Suites[0]
	if suite.Failures != 2 || suites.Failures != 2 {
		t.Fatalf("failures: suite=%d suites=%d", suite.Failures, suites.Failures)
	}
	var badStatus, conn *junitTestcase
	for i := range suite.Cases {
		switch {
		case strings.HasPrefix(suite.Cases[i].Name, "iter1 - bad status"):
			badStatus = &suite.Cases[i]
		case strings.HasPrefix(suite.Cases[i].Name, "iter1 - conn refused"):
			conn = &suite.Cases[i]
		}
	}
	if badStatus == nil || conn == nil {
		t.Fatalf("missing failing cases: %+v", suite.Cases)
	}
	// 断言失败：message 取断言错误，body 汇总全部失败断言
	if badStatus.Failure == nil || badStatus.Failure.Message != "500 != 2xx" {
		t.Fatalf("badStatus failure = %+v", badStatus.Failure)
	}
	if !strings.Contains(badStatus.Failure.Body, "status is 2xx") {
		t.Fatalf("failure body missing assertion name: %q", badStatus.Failure.Body)
	}
	if strings.Contains(badStatus.Failure.Body, "body has id") {
		t.Fatalf("failure body must list only failing assertions: %q", badStatus.Failure.Body)
	}
	// 网络错误：无断言，message/body 取 rr.Error
	if conn.Failure == nil || !strings.Contains(conn.Failure.Message, "dial refused") {
		t.Fatalf("conn failure = %+v", conn.Failure)
	}
}

func TestMarshalJUnitXMLSkippedAndEscaping(t *testing.T) {
	report := &Report{
		RunId: "r1", Total: 1, Passed: 1, Skipped: 3, DurationMs: 10,
		Results: []RequestResult{
			{Iteration: 1, RequestName: `get <user> & "friends"`, Status: 200},
		},
	}
	body, err := MarshalJUnitXML(report, "escape suite")
	if err != nil {
		t.Fatal(err)
	}
	suites := parseSuites(t, body)
	if suites.Skipped != 3 || suites.Suites[0].Skipped != 3 {
		t.Fatalf("skipped not propagated: %+v", suites)
	}
	// 名称里的 XML 特殊字符必须转义，不允许产生畸形 XML（unmarshal 成功已证明），
	// 且解码后名称原样保留
	if got := suites.Suites[0].Cases[0].Name; got != `iter1 - get <user> & "friends"` {
		t.Fatalf("case name round-trip = %q", got)
	}
}
