//go:build !windows

package secretstore

// Encrypt 在非 Windows 平台返回 ErrUnsupported
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	return nil, ErrUnsupported
}

// Decrypt 在非 Windows 平台返回 ErrUnsupported
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	return nil, ErrUnsupported
}
