// Package requesturl owns the query projection shared by request execution,
// code generation, and text exporters.
package requesturl

import (
	"net/url"
	"strings"

	"apirequest/backend/model"
)

// AddParams adds enabled parameters unless the same decoded key/value pair is
// already present. Distinct values for the same key remain multi-value query
// parameters.
func AddParams(query url.Values, params []model.KV) {
	seen := make(map[string]struct{})
	for key, values := range query {
		for _, value := range values {
			seen[pairKey(key, value)] = struct{}{}
		}
	}
	for _, parameter := range params {
		if !parameter.Enabled || parameter.Key == "" {
			continue
		}
		pair := pairKey(parameter.Key, parameter.Value)
		if _, exists := seen[pair]; exists {
			continue
		}
		query.Add(parameter.Key, parameter.Value)
		seen[pair] = struct{}{}
	}
}

// AppendParams preserves the caller's raw URL while appending missing query
// pairs. When encode is false, template placeholders remain untouched.
func AppendParams(raw string, params []model.KV, encode bool) string {
	fragment := ""
	if index := strings.IndexByte(raw, '#'); index >= 0 {
		fragment = raw[index:]
		raw = raw[:index]
	}
	queryStart := strings.IndexByte(raw, '?')
	base := raw
	rawQuery := ""
	if queryStart >= 0 {
		base = raw[:queryStart]
		rawQuery = raw[queryStart+1:]
	}

	seen := make(map[string]struct{})
	for _, item := range strings.Split(rawQuery, "&") {
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		key := decode(parts[0])
		value := ""
		if len(parts) == 2 {
			value = decode(parts[1])
		}
		seen[pairKey(key, value)] = struct{}{}
	}

	appended := make([]string, 0, len(params))
	for _, parameter := range params {
		if !parameter.Enabled || parameter.Key == "" {
			continue
		}
		pair := pairKey(parameter.Key, parameter.Value)
		if _, exists := seen[pair]; exists {
			continue
		}
		key, value := parameter.Key, parameter.Value
		if encode {
			key = url.QueryEscape(key)
			value = url.QueryEscape(value)
		}
		appended = append(appended, key+"="+value)
		seen[pair] = struct{}{}
	}
	if len(appended) == 0 {
		return raw + fragment
	}
	if rawQuery != "" {
		rawQuery += "&"
	}
	rawQuery += strings.Join(appended, "&")
	return base + "?" + rawQuery + fragment
}

// SetParam replaces every existing occurrence of key with one value while
// preserving unrelated raw query items and the fragment.
func SetParam(raw, key, value string, encode bool) string {
	if key == "" {
		return raw
	}
	fragment := ""
	if index := strings.IndexByte(raw, '#'); index >= 0 {
		fragment = raw[index:]
		raw = raw[:index]
	}
	queryStart := strings.IndexByte(raw, '?')
	base := raw
	rawQuery := ""
	if queryStart >= 0 {
		base = raw[:queryStart]
		rawQuery = raw[queryStart+1:]
	}

	items := make([]string, 0)
	for _, item := range strings.Split(rawQuery, "&") {
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if decode(parts[0]) != key {
			items = append(items, item)
		}
	}
	encodedKey, encodedValue := key, value
	if encode {
		encodedKey = url.QueryEscape(key)
		encodedValue = url.QueryEscape(value)
	}
	items = append(items, encodedKey+"="+encodedValue)
	return base + "?" + strings.Join(items, "&") + fragment
}

func pairKey(key, value string) string { return key + "\x00" + value }

func decode(value string) string {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}
