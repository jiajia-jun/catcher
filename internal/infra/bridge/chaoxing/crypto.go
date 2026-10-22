package chaoxing

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"catcher/internal/domain/entity"
)

const (
	// aesKey 是学习通登录表单的固定密钥，IV 与密钥相同（不是随机 IV）
	aesKey = "u2oh6Vu^HWe4_AES"
)

var phonePattern = regexp.MustCompile(`^\d{11}$`)

// validateCredential 在发请求前拦掉明显错误的账号形态
//
// 服务端对「账号错」和「密码错」返回同一句提示，不本地校验就只能看到
// 误导性的「用户名或密码错误」
func validateCredential(creds entity.PasswordCredential) error {
	phone := strings.TrimSpace(creds.Phone)
	if phone == "" || creds.Password == "" {
		return fmt.Errorf("%w: 手机号和密码不能为空", entity.ErrAuthFailed)
	}
	if !phonePattern.MatchString(phone) {
		return fmt.Errorf("%w: 学习通账号是 11 位手机号", entity.ErrAuthFailed)
	}
	return nil
}

// encryptAES 按 AES-128-CBC + PKCS7 + Base64 加密登录字段。
// 明文就是原文，不做 JSON 包装
func encryptAES(plaintext string) (string, error) {
	block, err := aes.NewCipher([]byte(aesKey))
	if err != nil {
		return "", err
	}
	padded, err := pkcs7Pad([]byte(plaintext), block.BlockSize())
	if err != nil {
		return "", err
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(aesKey)).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out), nil
}

func pkcs7Pad(data []byte, blockSize int) ([]byte, error) {
	if blockSize <= 0 || blockSize > 255 {
		return nil, fmt.Errorf("无效分块大小: %d", blockSize)
	}
	padding := blockSize - len(data)%blockSize
	return append(append([]byte(nil), data...), bytes.Repeat([]byte{byte(padding)}, padding)...), nil
}
