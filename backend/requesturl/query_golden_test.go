package requesturl

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// goldenVectors 双轨解析共享向量（testdata/url_query_golden.json）：
// 与 frontend/src/utils/urlQueryGolden.test.ts 共读同一份文件，
// 锁定"UI 表格显示的参数"与"引擎实际解析发送的参数"一致（docs/frontend.md §4）。
type goldenVectors struct {
	Description string `json:"description"`
	Notes       []string
	Cases       []struct {
		Name  string     `json:"name"`
		Query string     `json:"query"`
		Pairs [][]string `json:"pairs"`
	}
}

func loadGolden(t *testing.T) goldenVectors {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "url_query_golden.json"))
	if err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	var v goldenVectors
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse golden vectors: %v", err)
	}
	return v
}

// TestGoldenQueryParsing 用引擎同款解析路径（url.ParseQuery，即 u.Query() 的内部实现）
// 断言每个向量 case：url.Values 是按 key 聚合的 map，故按多集（key→值计数）比较。
func TestGoldenQueryParsing(t *testing.T) {
	v := loadGolden(t)
	if len(v.Cases) == 0 {
		t.Fatal("golden vector file has no cases")
	}
	for _, tc := range v.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			parsed, err := url.ParseQuery(tc.Query)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tc.Query, err)
			}
			want := map[string]map[string]int{}
			for _, pair := range tc.Pairs {
				if len(pair) != 2 {
					t.Fatalf("malformed pair %v", pair)
				}
				if want[pair[0]] == nil {
					want[pair[0]] = map[string]int{}
				}
				want[pair[0]][pair[1]]++
			}
			if len(parsed) != len(want) {
				t.Fatalf("parsed keys = %v, want %v", parsed, want)
			}
			for key, values := range parsed {
				wantValues, ok := want[key]
				if !ok {
					t.Fatalf("unexpected key %q in parsed query", key)
				}
				got := map[string]int{}
				for _, val := range values {
					got[val]++
				}
				if len(got) != len(wantValues) {
					t.Fatalf("key %q: got values %v, want %v", key, values, wantValues)
				}
				for val, count := range got {
					if wantValues[val] != count {
						t.Fatalf("key %q value %q: got %d occurrences, want %d", key, val, count, wantValues[val])
					}
				}
			}
		})
	}
}
