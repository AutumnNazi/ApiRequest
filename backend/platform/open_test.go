package platform

import (
	"context"
	"strings"
	"testing"
)

func TestOpenURLValidatesAndDelegates(t *testing.T) {
	ctx := context.Background()
	var opened string
	err := openURL(ctx, "  HTTPS://example.com/oauth?state=1  ", func(gotCtx context.Context, target string) {
		if gotCtx != ctx {
			t.Error("opener received a different context")
		}
		opened = target
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened != "https://example.com/oauth?state=1" {
		t.Fatalf("opened URL = %q", opened)
	}
}

func TestOpenURLRejectsUnsafeOrUnavailableOpeners(t *testing.T) {
	tests := []struct {
		name   string
		ctx    context.Context
		target string
		opener func(context.Context, string)
		match  string
	}{
		{name: "startup", target: "https://example.com", opener: func(context.Context, string) {}, match: "before application startup"},
		{name: "missing opener", ctx: context.Background(), target: "https://example.com", match: "not configured"},
		{name: "file scheme", ctx: context.Background(), target: "file:///tmp/token", opener: func(context.Context, string) {}, match: "http or https"},
		{name: "custom scheme", ctx: context.Background(), target: "javascript:alert(1)", opener: func(context.Context, string) {}, match: "http or https"},
		{name: "missing host", ctx: context.Background(), target: "https:///oauth", opener: func(context.Context, string) {}, match: "include a host"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := openURL(test.ctx, test.target, test.opener)
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("error = %v, want text %q", err, test.match)
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := openURL(canceled, "https://example.com", func(context.Context, string) {}); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("canceled context error = %v", err)
	}
}
