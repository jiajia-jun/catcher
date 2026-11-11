// Package secretstore 用 Windows DPAPI 加密本机凭据
package secretstore

import "errors"

// ErrUnsupported 表示当前构建平台没有 DPAPI 实现
var ErrUnsupported = errors.New("当前平台不支持 DPAPI 加密")

// entropy 是 DPAPI 的可选附加熵。值本身不是秘密，但解密时必须一致
var entropy = []byte("catcher:v1")

// Cipher 实现 domain.SecretCipher
type Cipher struct{}

func New() *Cipher { return &Cipher{} }
