package codegen

import (
	"strings"
	"testing"

	"apirequest/backend/model"
)

func binaryReq() model.HttpRequest {
	return model.HttpRequest{
		Method:   "POST",
		Url:      "https://upload.io/file",
		Body:     model.Body{Kind: "binary", Path: "/tmp/data.bin"},
		Auth:     model.Auth{Type: "none"},
		Settings: model.DefaultSettings(),
	}
}

func TestJavaBinaryUpload(t *testing.T) {
	out, err := Generate("java-httpclient", binaryReq())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "BodyPublishers.ofFile(java.nio.file.Paths.get(\"/tmp/data.bin\"))") {
		t.Errorf("java binary 缺 ofFile:\n%s", out)
	}
	if !strings.Contains(out, "application/octet-stream") {
		t.Errorf("java 缺 Content-Type:\n%s", out)
	}
}

func TestRustBinaryUpload(t *testing.T) {
	out, err := Generate("rust-reqwest", binaryReq())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "reqwest::Body::from(std::fs::read(\"/tmp/data.bin\")?)") {
		t.Errorf("rust 缺 std::fs::read:\n%s", out)
	}
	if !strings.Contains(out, "Result<(), Box<dyn Error>>") ||
		!strings.Contains(out, `.body(reqwest::Body::from(std::fs::read("/tmp/data.bin")?))`) {
		t.Errorf("rust binary 代码无法传播 io::Error 或括号不完整:\n%s", out)
	}
}

func TestPhpBinaryUpload(t *testing.T) {
	out, err := Generate("php-curl", binaryReq())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "file_get_contents('/tmp/data.bin')") || strings.Contains(out, "CURLFile") {
		t.Errorf("php binary 应发送 raw bytes:\n%s", out)
	}
}

func TestCsharpBinaryUpload(t *testing.T) {
	out, err := Generate("csharp-httpclient", binaryReq())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "new StreamContent(System.IO.File.OpenRead(\"/tmp/data.bin\"))") {
		t.Errorf("csharp 缺 StreamContent:\n%s", out)
	}
	if strings.Contains(out, `req.Headers.Add("Content-Type"`) ||
		!strings.Contains(out, `req.Content.Headers.TryAddWithoutValidation("Content-Type", "application/octet-stream")`) {
		t.Errorf("csharp Content-Type 必须写入 content headers:\n%s", out)
	}
}
