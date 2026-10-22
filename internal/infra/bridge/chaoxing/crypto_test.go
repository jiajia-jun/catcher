package chaoxing

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"strings"
	"testing"

	"catcher/internal/domain/entity"
)

// decryptAES 仅供测试断言加密结果可逆
func decryptAES(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("不是合法 Base64: %v", err)
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		t.Fatalf("密文长度 %d 不是分组倍数", len(raw))
	}
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, []byte(aesKey)).CryptBlocks(out, raw)
	pad := int(out[len(out)-1])
	if pad <= 0 || pad > aes.BlockSize || !bytes.Equal(out[len(out)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		t.Fatalf("PKCS7 填充无效: %v", out)
	}
	return string(out[:len(out)-pad])
}

func TestEncryptAESRoundTrip(t *testing.T) {
	for _, plain := range []string{"13800138000", "p@ssw0rd", strings.Repeat("长", 20)} {
		encrypted, err := encryptAES(plain)
		if err != nil {
			t.Fatal(err)
		}
		if got := decryptAES(t, encrypted); got != plain {
			t.Fatalf("往返 = %q, want %q", got, plain)
		}
	}
}

func TestEncryptAESDeterministic(t *testing.T) {
	// IV 等于密钥，同一明文必须得到同一密文；随机 IV 会破坏这个前提
	a, err := encryptAES("13800138000")
	if err != nil {
		t.Fatal(err)
	}
	b, err := encryptAES("13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("同明文加密结果不一致")
	}
}

func TestValidateCredential(t *testing.T) {
	cases := []struct {
		name  string
		creds entity.PasswordCredential
		ok    bool
	}{
		{"正常", entity.PasswordCredential{Phone: "13800138000", Password: "x"}, true},
		{"空密码", entity.PasswordCredential{Phone: "13800138000"}, false},
		{"10 位", entity.PasswordCredential{Phone: "1380013800", Password: "x"}, false},
		{"含字母", entity.PasswordCredential{Phone: "1380013800a", Password: "x"}, false},
	}
	for _, c := range cases {
		err := validateCredential(c.creds)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}
