package auth

import (
	"encoding/base64"
	"net/http"
)

// basicAuth Authorization: Basic base64(user:pass)
type basicAuth struct{}

func (basicAuth) Type() string { return "basic" }

func (basicAuth) Apply(req *http.Request, p map[string]string) error {
	cred := base64.StdEncoding.EncodeToString([]byte(p["username"] + ":" + p["password"]))
	req.Header.Set("Authorization", "Basic "+cred)
	return nil
}

// bearerAuth Authorization: Bearer <token>
type bearerAuth struct{}

func (bearerAuth) Type() string { return "bearer" }

func (bearerAuth) Apply(req *http.Request, p map[string]string) error {
	req.Header.Set("Authorization", "Bearer "+p["token"])
	return nil
}

// apiKeyAuth 注入到 header 或 query（params: key/value/in）
type apiKeyAuth struct{}

func (apiKeyAuth) Type() string { return "apikey" }

func (apiKeyAuth) Apply(req *http.Request, p map[string]string) error {
	key, value := p["key"], p["value"]
	if key == "" {
		return nil
	}
	if p["in"] == "query" {
		q := req.URL.Query()
		q.Set(key, value)
		req.URL.RawQuery = q.Encode()
	} else {
		req.Header.Set(key, value)
	}
	return nil
}

func init() {
	Register(basicAuth{})
	Register(bearerAuth{})
	Register(apiKeyAuth{})
}
