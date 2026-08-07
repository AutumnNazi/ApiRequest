package requesturl

import "testing"

func TestSetParamReplacesEveryDecodedKeyAndPreservesFragment(t *testing.T) {
	got := SetParam("https://x.io/items?api_key=old&tag=one&api%5Fkey=older#result", "api_key", "new key", true)
	want := "https://x.io/items?tag=one&api_key=new+key#result"
	if got != want {
		t.Fatalf("SetParam() = %q, want %q", got, want)
	}
}
