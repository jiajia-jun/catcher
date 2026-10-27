package emails

import (
	"bytes"
	"catcher/internal/domain/entity"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeCipher 用可识别的前缀模拟 DPAPI，测试里不依赖 Windows API
type fakeCipher struct{}

func (fakeCipher) Encrypt(plaintext []byte) ([]byte, error) {
	return append([]byte("enc:"), plaintext...), nil
}

func (fakeCipher) Decrypt(ciphertext []byte) ([]byte, error) {
	if !bytes.HasPrefix(ciphertext, []byte("enc:")) {
		return nil, errors.New("无效密文")
	}
	return ciphertext[len("enc:"):], nil
}

func testPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "email.json")
}

func TestInitEmpty(t *testing.T) {
	path := testPath(t)
	e, err := Init(path, fakeCipher{})
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if e.SenderAddr != "" || e.Password != "" || e.SMTP != "" {
		t.Fatalf("空文件应得到空配置: %+v", e)
	}
	if e.Path != path {
		t.Fatalf("路径未保留: %q", e.Path)
	}
}

func TestSetInfoPersistsEncrypted(t *testing.T) {
	path := testPath(t)
	e, err := Init(path, fakeCipher{})
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}
	if err := e.SetInfo(&entity.EmailSender{
		Emailer:  entity.Email{Addr: "sender@example.com", Smtp: "smtp.example.com"},
		PassWord: "auth-code",
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取配置文件失败: %v", err)
	}
	if bytes.Contains(raw, []byte("auth-code")) {
		t.Fatal("授权码不应明文落盘")
	}
	if !bytes.Contains(raw, []byte("secret")) {
		t.Fatalf("配置文件中缺少密文字段: %s", raw)
	}

	reloaded, err := Init(path, fakeCipher{})
	if err != nil {
		t.Fatalf("重新读取失败: %v", err)
	}
	info := reloaded.GetInfo()
	if info.Emailer.Addr != "sender@example.com" {
		t.Fatalf("发件地址不匹配: %q", info.Emailer.Addr)
	}
	if info.Emailer.Smtp != "smtp.example.com" {
		t.Fatalf("SMTP 不匹配: %q", info.Emailer.Smtp)
	}
	if info.PassWord != "auth-code" {
		t.Fatalf("授权码不匹配: %q", info.PassWord)
	}
	if reloaded.Path != path {
		t.Fatalf("路径未保留: %q", reloaded.Path)
	}
}

func TestSetInfoRequiresCipher(t *testing.T) {
	e := &Email{Path: testPath(t)}
	err := e.SetInfo(&entity.EmailSender{
		Emailer:  entity.Email{Addr: "sender@example.com", Smtp: "smtp.example.com"},
		PassWord: "auth-code",
	})
	if err == nil {
		t.Fatal("没有 cipher 时应拒绝保存授权码")
	}
}

// 配置损坏时仍应拿到可写对象，允许用户重新保存覆盖
func TestInitRecoversFromInvalidJSON(t *testing.T) {
	path := testPath(t)
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("写入损坏配置失败: %v", err)
	}
	e, err := Init(path, fakeCipher{})
	if err == nil {
		t.Fatal("损坏的 JSON 应返回错误")
	}
	if e == nil {
		t.Fatal("损坏的 JSON 仍应返回可用对象")
	}
	if err := e.SetInfo(&entity.EmailSender{
		Emailer:  entity.Email{Addr: "sender@example.com", Smtp: "smtp.example.com"},
		PassWord: "auth-code",
	}); err != nil {
		t.Fatalf("覆盖损坏配置失败: %v", err)
	}
	if _, err := Init(path, fakeCipher{}); err != nil {
		t.Fatalf("覆盖后重新读取失败: %v", err)
	}
}

// 旧版明文配置读取后应自动迁移为密文
func TestInitMigratesLegacyPlaintext(t *testing.T) {
	path := testPath(t)
	legacy := []byte(`{"senderAddr":"sender@example.com","smtp":"smtp.example.com","password":"legacy-code"}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}

	e, err := Init(path, fakeCipher{})
	if err != nil {
		t.Fatalf("读取旧配置失败: %v", err)
	}
	if e.Password != "legacy-code" {
		t.Fatalf("旧配置授权码读取失败: %q", e.Password)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移后的配置失败: %v", err)
	}
	if bytes.Contains(raw, []byte("legacy-code")) {
		t.Fatalf("迁移后仍存在明文授权码: %s", raw)
	}
	if !bytes.Contains(raw, []byte("secret")) {
		t.Fatalf("迁移后缺少密文字段: %s", raw)
	}
}

// 密文无法解密时保留地址与 SMTP，让用户只需重填授权码
func TestInitDecryptFailureAllowsRewrite(t *testing.T) {
	path := testPath(t)
	broken := []byte(`{"senderAddr":"sender@example.com","smtp":"smtp.example.com","secret":"bm90LWVuY3J5cHRlZA=="}`)
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatalf("写入损坏密文失败: %v", err)
	}

	e, err := Init(path, fakeCipher{})
	if err == nil {
		t.Fatal("无效密文应返回错误")
	}
	if e == nil || e.SenderAddr != "sender@example.com" || e.SMTP != "smtp.example.com" {
		t.Fatalf("应保留地址与 SMTP: %+v", e)
	}
	if e.Password != "" {
		t.Fatalf("解密失败时不应带出授权码: %q", e.Password)
	}
	if err := e.SetInfo(&entity.EmailSender{
		Emailer:  entity.Email{Addr: e.SenderAddr, Smtp: e.SMTP},
		PassWord: "new-code",
	}); err != nil {
		t.Fatalf("重新写入失败: %v", err)
	}
	reloaded, err := Init(path, fakeCipher{})
	if err != nil {
		t.Fatalf("重新写入后读取失败: %v", err)
	}
	if reloaded.Password != "new-code" {
		t.Fatalf("重新写入的授权码不匹配: %q", reloaded.Password)
	}
}

// 真实发送会连接外部 SMTP 服务器，默认跳过；需要时设置下面三个环境变量
func TestSendEmailLive(t *testing.T) {
	addr := os.Getenv("CATCHER_TEST_EMAIL_ADDR")
	smtpHost := os.Getenv("CATCHER_TEST_EMAIL_SMTP")
	password := os.Getenv("CATCHER_TEST_EMAIL_PASSWORD")
	if addr == "" || smtpHost == "" || password == "" {
		t.Skip("未设置 CATCHER_TEST_EMAIL_* 环境变量，跳过真实发送测试")
	}

	e := &Email{
		SenderAddr: addr,
		Password:   password,
		SMTP:       smtpHost,
	}
	if err := e.SendEmail(&entity.EmailContent{
		Topic: "Catcher 测试",
		Desc:  "hello world",
	}); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
}
