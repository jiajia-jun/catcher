//go:build windows

package secretstore

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Encrypt 用当前 Windows 用户的 DPAPI 密钥加密明文
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	return transform(true, plaintext)
}

// Decrypt 解密 Encrypt 产出的密文。密文损坏或换了用户时返回错误
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	return transform(false, ciphertext)
}

func transform(encrypt bool, input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("DPAPI 输入为空")
	}

	in := windows.DataBlob{Size: uint32(len(input)), Data: &input[0]}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob

	var err error
	if encrypt {
		err = windows.CryptProtectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		if encrypt {
			return nil, fmt.Errorf("DPAPI 加密失败: %w", err)
		}
		return nil, fmt.Errorf("DPAPI 解密失败: %w", err)
	}
	defer func() {
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	}()

	if out.Data == nil || out.Size == 0 {
		return nil, fmt.Errorf("DPAPI 返回空结果")
	}
	// 复制一份，避免把 DPAPI 分配的内存交出去
	buf := unsafe.Slice(out.Data, int(out.Size))
	result := make([]byte, len(buf))
	copy(result, buf)
	return result, nil
}
