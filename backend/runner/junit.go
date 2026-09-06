// JUnit XML 导出：供 CI 测试报告（GitHub Actions test-report 等）消费。
// 每个 Results 条目对应一个 testcase；被 StopOnError/取消跳过的执行不产生
// testcase，只体现在 skipped 属性上（与 Report.Skipped 的"未执行"语义一致）。
package runner

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitTestcase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr,omitempty"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitTestsuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Skipped  int             `xml:"skipped,attr"`
	Time     string          `xml:"time,attr,omitempty"`
	Cases    []junitTestcase `xml:"testcase"`
}

type junitTestsuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Skipped  int              `xml:"skipped,attr"`
	Time     string           `xml:"time,attr,omitempty"`
	Suites   []junitTestsuite `xml:"testsuite"`
}

func junitSeconds(ms int64) string {
	return fmt.Sprintf("%.3f", float64(ms)/1000)
}

// MarshalJUnitXML 把运行报告序列化为 JUnit XML。suiteName 用于 testsuite/classname
// 标识（一般传集合名）。
func MarshalJUnitXML(report *Report, suiteName string) ([]byte, error) {
	cases := make([]junitTestcase, 0, len(report.Results))
	failures := 0
	for _, rr := range report.Results {
		tc := junitTestcase{
			Name:      fmt.Sprintf("iter%d - %s", rr.Iteration, rr.RequestName),
			Classname: suiteName,
			Time:      junitSeconds(rr.DurationMs),
		}
		if rr.Failed {
			failures++
			tc.Failure = &junitFailure{Message: junitFailureMessage(rr)}
			tc.Failure.Body = junitFailureBody(rr)
		}
		cases = append(cases, tc)
	}
	doc := junitTestsuites{
		Name:     "apirequest",
		Tests:    report.Total,
		Failures: report.Failed,
		Skipped:  report.Skipped,
		Time:     junitSeconds(report.DurationMs),
		Suites: []junitTestsuite{{
			Name:     suiteName,
			Tests:    len(cases),
			Failures: failures,
			Skipped:  report.Skipped,
			Time:     junitSeconds(report.DurationMs),
			Cases:    cases,
		}},
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

// junitFailureMessage 单行摘要：优先断言错误，其次网络错误
func junitFailureMessage(rr RequestResult) string {
	for _, t := range rr.TestResults {
		if !t.Pass {
			if t.Error != "" {
				return t.Error
			}
			return t.Name
		}
	}
	if rr.Error != "" {
		return rr.Error
	}
	return "request failed"
}

// junitFailureBody 完整失败明细：逐行列出失败断言；无断言时用网络错误
func junitFailureBody(rr RequestResult) string {
	var lines []string
	for _, t := range rr.TestResults {
		if t.Pass {
			continue
		}
		if t.Error != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", t.Name, t.Error))
		} else {
			lines = append(lines, t.Name)
		}
	}
	if len(lines) == 0 && rr.Error != "" {
		lines = append(lines, rr.Error)
	}
	return strings.Join(lines, "\n")
}
