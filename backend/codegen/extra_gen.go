package codegen

import (
	"encoding/base64"
	"strings"

	"apirequest/backend/model"
)

// binaryPath 返回 binary body 的文件路径（仅 Kind=binary 且 Path 非空时）；
// 供 Java/Rust/PHP/C# 4 个生成器在上传场景使用。
func binaryPath(req model.HttpRequest) (path string, ok bool) {
	if req.Body.Kind == "binary" && req.Body.Path != "" {
		return req.Body.Path, true
	}
	return "", false
}

// hasBinaryBody 是否有 binary 上传 body
func hasBinaryBody(req model.HttpRequest) bool { _, ok := binaryPath(req); return ok }

func addBodyContentType(req model.HttpRequest, headers []model.KV, textContentType string, hasTextBody bool) []model.KV {
	if hasHeader(req, "Content-Type") {
		return headers
	}
	if hasTextBody && textContentType != "" {
		return append(headers, model.KV{Key: "Content-Type", Value: textContentType})
	}
	if hasBinaryBody(req) {
		return append(headers, model.KV{Key: "Content-Type", Value: "application/octet-stream"})
	}
	return headers
}

// ── Java (java.net.http.HttpClient, JDK 11+) ──

type javaGen struct{}

func (javaGen) Id() string   { return "java-httpclient" }
func (javaGen) Name() string { return "Java (HttpClient)" }

func (javaGen) Generate(req model.HttpRequest) string {
	text, ct, hasBody := bodyText(req)
	headers := addBodyContentType(req, enabledHeaders(req), ct, hasBody)
	if req.Auth.Type == "basic" {
		cred := base64Std(req.Auth.Params["username"] + ":" + req.Auth.Params["password"])
		headers = append(headers, model.KV{Key: "Authorization", Value: "Basic " + cred})
	}

	var b strings.Builder
	b.WriteString("import java.net.URI;\n")
	b.WriteString("import java.net.http.HttpClient;\n")
	b.WriteString("import java.net.http.HttpRequest;\n")
	b.WriteString("import java.net.http.HttpResponse;\n\n")
	b.WriteString("HttpClient client = HttpClient.newHttpClient();\n\n")

	// Java Builder 链：.uri(...) + .method(...) + .header(...)
	b.WriteString("HttpRequest request = HttpRequest.newBuilder()\n")
	b.WriteString("    .uri(URI.create(" + javaQuote(fullUrl(req)) + "))\n")
	method := strings.ToUpper(orGET(req.Method))
	if hasBody && text != "" {
		b.WriteString("    .method(" + javaQuote(method) + ", HttpRequest.BodyPublishers.ofString(" + javaQuote(text) + "))\n")
	} else if bp, ok := binaryPath(req); ok {
		// binary 上传：用 BodyPublishers.ofFile
		b.WriteString("    .method(" + javaQuote(method) + ", HttpRequest.BodyPublishers.ofFile(java.nio.file.Paths.get(" + javaQuote(bp) + ")))\n")
	} else {
		b.WriteString("    .method(" + javaQuote(method) + ", HttpRequest.BodyPublishers.noBody())\n")
	}
	for _, h := range headers {
		b.WriteString("    .header(" + javaQuote(h.Key) + ", " + javaQuote(h.Value) + ")\n")
	}
	b.WriteString("    .build();\n\n")
	b.WriteString("HttpResponse<String> response = client.send(request, HttpResponse.BodyHandlers.ofString());\n")
	b.WriteString("System.out.println(response.statusCode());\nSystem.out.println(response.body());")
	return b.String()
}

func javaQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// ── Rust (reqwest, async) ──

type rustGen struct{}

func (rustGen) Id() string   { return "rust-reqwest" }
func (rustGen) Name() string { return "Rust (reqwest)" }

func (rustGen) Generate(req model.HttpRequest) string {
	text, ct, hasBody := bodyText(req)
	headers := addBodyContentType(req, enabledHeaders(req), ct, hasBody)
	if req.Auth.Type == "basic" {
		// Rust reqwest 把 basic 转成 Authorization 头
		cred := base64Std(req.Auth.Params["username"] + ":" + req.Auth.Params["password"])
		headers = append(headers, model.KV{Key: "Authorization", Value: "Basic " + cred})
	}

	var b strings.Builder
	b.WriteString("use std::error::Error;\n\n")
	b.WriteString("async fn run() -> Result<(), Box<dyn Error>> {\n")
	if req.Settings.VerifyTLS {
		b.WriteString("    let client = reqwest::Client::new();\n")
	} else {
		b.WriteString("    let client = reqwest::Client::builder()\n")
		b.WriteString("        .danger_accept_invalid_certs(true)\n")
		b.WriteString("        .build()?;\n")
	}
	method := strings.ToUpper(orGET(req.Method))
	b.WriteString("    let req = client.request(" + rustQuote(method) + ".parse()?, " + rustQuote(fullUrl(req)) + ")")
	if len(headers) > 0 || (hasBody && text != "") || hasBinaryBody(req) {
		b.WriteByte('\n')
		// header
		for _, h := range headers {
			b.WriteString("        .header(" + rustQuote(h.Key) + ", " + rustQuote(h.Value) + ")\n")
		}
		if hasBody && text != "" {
			b.WriteString("        .body(" + rustQuote(text) + ")\n")
		} else if bp, ok := binaryPath(req); ok {
			// binary 上传：读文件为字节后 body（async 上下文）
			b.WriteString("        .body(reqwest::Body::from(std::fs::read(" + rustQuote(bp) + ")?))\n")
		}
		b.WriteString("        ;\n")
	} else {
		b.WriteString(";\n")
	}
	b.WriteString("    let resp = req.send().await?\n")
	b.WriteString("        .text().await?;\n")
	b.WriteString("    println!(\"{}\", resp);\n    Ok(())\n}")
	return b.String()
}

func rustQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

// ── PHP (cURL extension) ──

type phpGen struct{}

func (phpGen) Id() string   { return "php-curl" }
func (phpGen) Name() string { return "PHP (cURL)" }

func (phpGen) Generate(req model.HttpRequest) string {
	text, ct, hasBody := bodyText(req)
	headers := addBodyContentType(req, enabledHeaders(req), ct, hasBody)
	if req.Auth.Type == "basic" {
		headers = append(headers, model.KV{Key: "Authorization", Value: "Basic " + base64Std(req.Auth.Params["username"]+":"+req.Auth.Params["password"])})
	}

	var b strings.Builder
	b.WriteString("<?php\n")
	b.WriteString("$ch = curl_init(" + phpQuote(fullUrl(req)) + ");\n")
	b.WriteString("curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);\n")
	b.WriteString("curl_setopt($ch, CURLOPT_CUSTOMREQUEST, " + phpQuote(orGET(req.Method)) + ");\n")

	if len(headers) > 0 {
		b.WriteString("curl_setopt($ch, CURLOPT_HTTPHEADER, [\n")
		for _, h := range headers {
			b.WriteString("    " + phpQuote(h.Key+": "+h.Value) + ",\n")
		}
		b.WriteString("]);\n")
	}
	if hasBody && text != "" {
		b.WriteString("curl_setopt($ch, CURLOPT_POSTFIELDS, " + phpQuote(text) + ");\n")
	} else if bp, ok := binaryPath(req); ok {
		// binary body 是原始字节，不是 multipart file 字段。
		b.WriteString("$data = file_get_contents(" + phpQuote(bp) + ");\n")
		b.WriteString("if ($data === false) { throw new RuntimeException('Unable to read binary body'); }\n")
		b.WriteString("curl_setopt($ch, CURLOPT_POSTFIELDS, $data);\n")
	}
	if !req.Settings.VerifyTLS {
		b.WriteString("curl_setopt($ch, CURLOPT_SSL_VERIFYPEER, false);\n")
		b.WriteString("curl_setopt($ch, CURLOPT_SSL_VERIFYHOST, 0);\n")
	}
	b.WriteString("\n$response = curl_exec($ch);\n")
	b.WriteString("$status = curl_getinfo($ch, CURLINFO_HTTP_CODE);\n")
	b.WriteString("curl_close($ch);\n\n")
	b.WriteString("echo $status . \"\\n\";\necho $response . \"\\n\";")
	return b.String()
}

func phpQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\r", `'."\r".'`)
	s = strings.ReplaceAll(s, "\n", `'."\n".'`)
	s = strings.ReplaceAll(s, "\t", `'."\t".'`)
	return "'" + s + "'"
}

// ── C# (HttpClient) ──

type csharpGen struct{}

func (csharpGen) Id() string   { return "csharp-httpclient" }
func (csharpGen) Name() string { return "C# (HttpClient)" }

func (csharpGen) Generate(req model.HttpRequest) string {
	text, ct, hasBody := bodyText(req)
	headers := addBodyContentType(req, enabledHeaders(req), ct, hasBody)
	if req.Auth.Type == "basic" {
		cred := base64Std(req.Auth.Params["username"] + ":" + req.Auth.Params["password"])
		headers = append(headers, model.KV{Key: "Authorization", Value: "Basic " + cred})
	}

	var b strings.Builder
	b.WriteString("using System;\n")
	b.WriteString("using System.Net.Http;\n\n")
	if req.Settings.VerifyTLS {
		b.WriteString("using var client = new HttpClient();\n")
	} else {
		b.WriteString("using var handler = new HttpClientHandler\n")
		b.WriteString("{\n    ServerCertificateCustomValidationCallback = HttpClientHandler.DangerousAcceptAnyServerCertificateValidator,\n};\n")
		b.WriteString("using var client = new HttpClient(handler);\n")
	}
	meth := strings.ToUpper(strings.TrimSpace(req.Method))
	if meth == "" {
		meth = "GET"
	}
	b.WriteString("using var req = new HttpRequestMessage(new HttpMethod(" + csharpQuote(meth) + "), " + csharpQuote(fullUrl(req)) + ");\n")

	hasContent := false
	if hasBody && text != "" {
		b.WriteString("req.Content = new StringContent(" + csharpQuote(text) + ");\n")
		hasContent = true
	} else if bp, ok := binaryPath(req); ok {
		// binary 上传：StreamContent + File.OpenRead
		b.WriteString("req.Content = new StreamContent(System.IO.File.OpenRead(" + csharpQuote(bp) + "));\n")
		hasContent = true
	}
	for _, h := range headers {
		if strings.HasPrefix(strings.ToLower(h.Key), "content-") {
			if !hasContent {
				b.WriteString("req.Content = new ByteArrayContent(Array.Empty<byte>());\n")
				hasContent = true
			}
			b.WriteString("req.Content.Headers.Remove(" + csharpQuote(h.Key) + ");\n")
			b.WriteString("req.Content.Headers.TryAddWithoutValidation(" + csharpQuote(h.Key) + ", " + csharpQuote(h.Value) + ");\n")
		} else {
			b.WriteString("req.Headers.TryAddWithoutValidation(" + csharpQuote(h.Key) + ", " + csharpQuote(h.Value) + ");\n")
		}
	}
	b.WriteString("\nvar resp = await client.SendAsync(req);\n")
	b.WriteString("resp.EnsureSuccessStatusCode();\n")
	b.WriteString("Console.WriteLine(await resp.Content.ReadAsStringAsync());")
	return b.String()
}

func csharpQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}
