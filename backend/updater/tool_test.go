package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// keygen + sign 工具链与 Check 验签闭环：签名过的 manifest 必须通过验证
func TestUpdatetoolSignRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "stable.json")
	manifest := []byte(`{"version":"1.2.0","platforms":{}}`)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "updatetool.exe")
	if out, err := exec.Command("go", "build", "-o", bin, "apirequest/cmd/updatetool").CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin, "sign", manifestPath, toHex(priv)).CombinedOutput(); err != nil {
		t.Fatalf("sign: %v\n%s", err, out)
	}

	sigRaw, err := os.ReadFile(manifestPath + ".sig")
	if err != nil {
		t.Fatalf("sig file: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(string(sigRaw))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, manifest, sig) {
		t.Fatal("signature from tool does not verify")
	}
}

func toHex(b []byte) string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexDigits[v>>4]
		out[i*2+1] = hexDigits[v&0x0f]
	}
	return string(out)
}
