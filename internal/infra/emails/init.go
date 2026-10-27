package emails

import (
	"bytes"
	"catcher/internal/domain"
	"catcher/internal/domain/entity"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const DefaultPath string = "./.secret"

type Email struct {
	SenderAddr string
	// Password 只存在于进程内存，落盘时用 cipher 加密
	Password string
	SMTP     string
	Path     string
	cipher   domain.SecretCipher
}

// storedEmail 是落盘格式。授权码以 DPAPI 密文保存；
// password 字段仅用于兼容旧版明文配置，读取后会迁移为密文
type storedEmail struct {
	SenderAddr string `json:"senderAddr"`
	SMTP       string `json:"smtp"`
	Secret     string `json:"secret,omitempty"`
	Password   string `json:"password,omitempty"`
}

// Init 读取邮箱配置。文件不存在或内容为空时返回一份空配置，
// 由后续 SetInfo 创建文件。JSON 损坏或密文无法解密时仍返回可用对象，
// 让上层可以提示用户重新填写并覆盖损坏的文件
func Init(path string, cipher domain.SecretCipher) (*Email, error) {
	if path == "" {
		path = DefaultPath
	}
	e := &Email{Path: path, cipher: cipher}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return e, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return e, nil
	}

	var stored storedEmail
	if err := json.Unmarshal(data, &stored); err != nil {
		return e, fmt.Errorf("解析邮箱配置失败: %w", err)
	}
	e.SenderAddr = stored.SenderAddr
	e.SMTP = stored.SMTP

	switch {
	case stored.Secret != "":
		if cipher == nil {
			return e, fmt.Errorf("缺少凭据解密组件，请重新填写邮箱授权码")
		}
		raw, err := base64.StdEncoding.DecodeString(stored.Secret)
		if err != nil {
			return e, fmt.Errorf("邮箱授权码格式异常，请重新填写: %w", err)
		}
		plain, err := cipher.Decrypt(raw)
		if err != nil {
			return e, fmt.Errorf("解密邮箱授权码失败，请重新填写: %w", err)
		}
		e.Password = string(plain)
	case stored.Password != "":
		// 旧版明文配置：先读入内存，再静默迁移为密文
		e.Password = stored.Password
		if cipher != nil {
			_ = e.SetInfo(&entity.EmailSender{
				Emailer:  entity.Email{Addr: e.SenderAddr, Smtp: e.SMTP},
				PassWord: e.Password,
			})
		}
	}
	return e, nil
}

// GetInfo 返回当前配置的副本，授权码只在进程内使用
func (E *Email) GetInfo() *entity.EmailSender {
	if E == nil {
		return &entity.EmailSender{}
	}
	return &entity.EmailSender{
		Emailer: entity.Email{
			Addr: E.SenderAddr,
			Smtp: E.SMTP,
		},
		PassWord: E.Password,
	}
}

// SetInfo 覆盖保存邮箱配置。授权码必须加密后才能落盘，
// 没有可用的 cipher 时直接拒绝，避免退回明文
func (E *Email) SetInfo(info *entity.EmailSender) error {
	if info == nil {
		return fmt.Errorf("邮箱配置为空")
	}
	if info.PassWord == "" {
		return fmt.Errorf("授权码不能为空")
	}
	if E.cipher == nil {
		return fmt.Errorf("当前环境不支持安全存储，无法保存邮箱授权码")
	}

	encrypted, err := E.cipher.Encrypt([]byte(info.PassWord))
	if err != nil {
		return fmt.Errorf("加密邮箱授权码失败: %w", err)
	}

	path := E.Path
	if path == "" {
		path = DefaultPath
		E.Path = path
	}

	stored := storedEmail{
		SenderAddr: info.Emailer.Addr,
		SMTP:       info.Emailer.Smtp,
		Secret:     base64.StdEncoding.EncodeToString(encrypted),
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}

	E.SenderAddr = info.Emailer.Addr
	E.SMTP = info.Emailer.Smtp
	E.Password = info.PassWord
	return nil
}
