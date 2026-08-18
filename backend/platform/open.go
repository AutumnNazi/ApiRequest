package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// OpenURL validates and opens an HTTP(S) URL with the user's default browser.
// Restricting schemes prevents a remote authorization endpoint from turning
// this capability into an arbitrary local-file or custom-protocol launcher.
func OpenURL(ctx context.Context, rawURL string) error {
	return openURL(ctx, rawURL, wailsruntime.BrowserOpenURL)
}

func openURL(ctx context.Context, rawURL string, opener func(context.Context, string)) error {
	if ctx == nil {
		return errors.New("browser opener is not available before application startup")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("browser opener context: %w", err)
	}
	if opener == nil {
		return errors.New("browser opener is not configured")
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid browser URL: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("browser URL must use http or https and include a host")
	}
	opener(ctx, parsed.String())
	return nil
}
