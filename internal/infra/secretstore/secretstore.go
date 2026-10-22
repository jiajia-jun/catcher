// Package secretstore 用 Windows DPAPI 加密本机凭据
//
// 约定（设计文档 §11.2）：
//   - 密文只绑定当前 Windows 用户；换用户或换机器后必须重新登录；
//   - 加密时带固定 entropy，降低同机其它程序直接解密的可能；
//   - 解密失败视为登录态不可用，绝不回退到明文
package secretstore

import "errors"

// ErrUnsupported 表示当前构建平台没有 DPAPI 实现
var ErrUnsupported = errors.New("当前平台不支持 DPAPI 加密")

// entropy 是 DPAPI 的可选附加熵。值本身不是秘密，但解密时必须一致
var entropy = []byte("catcher:v1")

// Cipher 实现 domain.SecretCipher
type Cipher struct{}

func New() *Cipher { return &Cipher{} }
