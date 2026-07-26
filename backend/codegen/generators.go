package codegen

import (
	"encoding/base64"
	"fmt"
	"strings"

	"apirequest/backend/model"
)

// ── curl ──

type curlGen struct{}

func (curlGen) Id() string   { return "curl" }
func (curlGen) Name() string { return "cURL" }

func (curlGen) Generate(req model.HttpRequest) string {
	var b strings.Builder
	b.WriteString("curl")
	if req.Method != "GET" && req.Method != "" {
		b.WriteString(" -X " + req.Method)
	}
	b.WriteString(" '" + shellQuote(fullUrl(req)) + "'")
	for _, h := range enabledHeaders(req) {
		b.WriteString(" \\\n  -H '" + shellQuote(h.Key+": "+h.Value) + "'")
	}
	if req.Auth.Type == "basic" {
		b.WriteString(" \\\n  -u '" + shellQuote(req.Auth.Params["username"]+":"+req.Auth.Params["password"]) + "'")
	}
	if text, ct, ok := bodyText(req); ok && text != "" {
		if ct != "" && !hasHeader(req, "Content-Type") {
			b.WriteString(" \\\n  -H 'Content-Type: " + ct + "'")
		}
		b.WriteString(" \\\n  -d '" + shellQuote(text) + "'")
	}
	if !req.Settings.VerifyTLS {
		b.WriteString(" \\\n  -k")
	}
	return b.String()
}

func shellQuote(s string) string { return strings.ReplaceAll(s, "'", `'\''`) }

func hasHeader(req model.HttpRequest, key string) bool {
	for _, h := range req.Headers {
		if h.Enabled && strings.EqualFold(h.Key, key) {
			return true
		}
	}
	return false
}

// ── JavaScript fetch ──

type fetchGen struct{}

func (fetchGen) Id() string   { return "javascript-fetch" }
func (fetchGen) Name() string { return "JavaScript (fetch)" }

func (fetchGen) Generate(req model.HttpRequest) string {
	var b strings.Builder
	b.WriteString("const response = await fetch(" + jsonQuote(fullUrl(req)) + ", {\n")
	b.WriteString("  method: " + jsonQuote(orGET(req.Method)) + ",\n")
	headers := enabledHeaders(req)
	text, ct, hasBody := bodyText(req)
	if req.Auth.Type == "basic" {
		cred := base64.StdEncoding.EncodeToString([]byte(req.Auth.Params["username"] + ":" + req.Auth.Params["password"]))
		headers = append(headers, model.KV{Key: "Authorization", Value: "Basic " + cred})
	}
	if hasBody && ct != "" && !hasHeader(req, "Content-Type") {
		headers = append(headers, model.KV{Key: "Content-Type", Value: ct})
	}
	if len(headers) > 0 {
		b.WriteString("  headers: {\n")
		for _, h := range headers {
			b.WriteString("    " + jsonQuote(h.Key) + ": " + jsonQuote(h.Value) + ",\n")
		}
		b.WriteString("  },\n")
	}
	if hasBody && text != "" {
		b.WriteString("  body: " + jsonQuote(text) + ",\n")
	}
	b.WriteString("});\n\nconst data = await response.json();\nconsole.log(data);")
	return b.String()
}

func orGET(m string) string {
	if m == "" {
		return "GET"
	}
	return m
}

// ── Python requests ──

type pythonGen struct{}

func (pythonGen) Id() string   { return "python-requests" }
func (pythonGen) Name() string { return "Python (requests)" }

func (pythonGen) Generate(req model.HttpRequest) string {
	var b strings.Builder
	b.WriteString("import requests\n\n")
	b.WriteString("url = " + pyQuote(fullUrl(req)) + "\n")

	headers := enabledHeaders(req)
	text, ct, hasBody := bodyText(req)
	if hasBody && ct != "" && !hasHeader(req, "Content-Type") {
		headers = append(headers, model.KV{Key: "Content-Type", Value: ct})
	}
	if len(headers) > 0 {
		b.WriteString("headers = {\n")
		for _, h := range headers {
			b.WriteString("    " + pyQuote(h.Key) + ": " + pyQuote(h.Value) + ",\n")
		}
		b.WriteString("}\n")
	}

	args := []string{"url"}
	if len(headers) > 0 {
		args = append(args, "headers=headers")
	}
	if hasBody && text != "" {
		if req.Body.Kind == "raw" && req.Body.Language == "json" {
			b.WriteString("payload = " + pyQuote(text) + "\n")
			args = append(args, "data=payload")
		} else {
			b.WriteString("data = " + pyQuote(text) + "\n")
			args = append(args, "data=data")
		}
	}
	if req.Auth.Type == "basic" {
		args = append(args, fmt.Sprintf("auth=(%s, %s)",
			pyQuote(req.Auth.Params["username"]), pyQuote(req.Auth.Params["password"])))
	}
	if !req.Settings.VerifyTLS {
		args = append(args, "verify=False")
	}

	method := strings.ToLower(orGET(req.Method))
	b.WriteString("\nresponse = requests." + method + "(" + strings.Join(args, ", ") + ")\n")
	b.WriteString("print(response.status_code)\nprint(response.text)")
	return b.String()
}

func pyQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

// ── Go net/http ──

type goGen struct{}

func (goGen) Id() string   { return "go-nethttp" }
func (goGen) Name() string { return "Go (net/http)" }

func (goGen) Generate(req model.HttpRequest) string {
	var b strings.Builder
	text, ct, hasBody := bodyText(req)

	b.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"io\"\n\t\"net/http\"\n")
	if hasBody && text != "" {
		b.WriteString("\t\"strings\"\n")
	}
	b.WriteString(")\n\nfunc main() {\n")

	if hasBody && text != "" {
		b.WriteString("\tbody := strings.NewReader(" + goQuote(text) + ")\n")
		b.WriteString("\treq, err := http.NewRequest(" + goQuote(orGET(req.Method)) + ", " + goQuote(fullUrl(req)) + ", body)\n")
	} else {
		b.WriteString("\treq, err := http.NewRequest(" + goQuote(orGET(req.Method)) + ", " + goQuote(fullUrl(req)) + ", nil)\n")
	}
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")

	headers := enabledHeaders(req)
	if hasBody && ct != "" && !hasHeader(req, "Content-Type") {
		headers = append(headers, model.KV{Key: "Content-Type", Value: ct})
	}
	for _, h := range headers {
		b.WriteString("\treq.Header.Set(" + goQuote(h.Key) + ", " + goQuote(h.Value) + ")\n")
	}
	if req.Auth.Type == "basic" {
		b.WriteString("\treq.SetBasicAuth(" + goQuote(req.Auth.Params["username"]) + ", " + goQuote(req.Auth.Params["password"]) + ")\n")
	}

	b.WriteString("\n\tresp, err := http.DefaultClient.Do(req)\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\tdefer resp.Body.Close()\n\n")
	b.WriteString("\tdata, _ := io.ReadAll(resp.Body)\n")
	b.WriteString("\tfmt.Println(resp.Status)\n\tfmt.Println(string(data))\n}")
	return b.String()
}

func goQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

func init() {
	Register(curlGen{})
	Register(fetchGen{})
	Register(pythonGen{})
	Register(goGen{})
}
