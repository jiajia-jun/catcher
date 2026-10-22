//go:build !windows

package secretstore

// Encrypt 在非 Windows 平台不可用
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	return nil, ErrUnsupported
}

// Decrypt 在非 Windows 平台不可用
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	return nil, ErrUnsupported
}
