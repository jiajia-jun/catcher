//go:build windows

package secretstore

import (
	"bytes"
	"testing"
)

func TestCipherRoundTrip(t *testing.T) {
	c := New()
	plain := []byte(`{"sessionid":"abc","auth":"def"}`)

	sealed, err := c.Encrypt(plain)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if bytes.Equal(sealed, plain) {
		t.Fatal("密文不应等于明文")
	}

	got, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("解密结果不一致: %q", got)
	}
}

func TestDecryptRejectsGarbage(t *testing.T) {
	c := New()
	if _, err := c.Decrypt([]byte("not-a-dpapi-blob")); err == nil {
		t.Fatal("损坏的密文应当报错，而不是返回明文")
	}
}

func TestEncryptRejectsEmpty(t *testing.T) {
	c := New()
	if _, err := c.Encrypt(nil); err == nil {
		t.Fatal("空输入应当报错")
	}
}
