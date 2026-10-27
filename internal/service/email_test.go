package service

import (
	"testing"

	"catcher/internal/domain/entity"
)

type fakeEmail struct {
	info entity.EmailSender
	sent []entity.EmailContent
}

func (f *fakeEmail) GetInfo() *entity.EmailSender {
	info := f.info
	return &info
}

func (f *fakeEmail) SetInfo(info *entity.EmailSender) error {
	f.info = *info
	return nil
}

func (f *fakeEmail) SendEmail(desc *entity.EmailContent) error {
	f.sent = append(f.sent, *desc)
	return nil
}

func TestEmailSettingsSavesAndKeepsPassword(t *testing.T) {
	mailer := &fakeEmail{}
	svc := New(Deps{Email: mailer})

	if _, configured := svc.EmailSettings(); configured {
		t.Fatal("空配置不应判定为已配置")
	}
	if err := svc.SaveEmailSettings(entity.EmailSender{
		Emailer:  entity.Email{Addr: "me@example.com", Smtp: "smtp.example.com"},
		PassWord: "auth-code",
	}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	info, configured := svc.EmailSettings()
	if !configured || info.PassWord != "auth-code" {
		t.Fatalf("保存后配置不正确: %+v", info)
	}

	// 只改 SMTP 时留空授权码，应沿用旧值
	if err := svc.SaveEmailSettings(entity.EmailSender{
		Emailer: entity.Email{Addr: "me@example.com", Smtp: "smtp2.example.com"},
	}); err != nil {
		t.Fatalf("更新 SMTP 失败: %v", err)
	}
	info, _ = svc.EmailSettings()
	if info.PassWord != "auth-code" {
		t.Fatalf("空授权码不应清空已保存的值: %q", info.PassWord)
	}
	if info.Emailer.Smtp != "smtp2.example.com" {
		t.Fatalf("SMTP 未更新: %q", info.Emailer.Smtp)
	}
}

func TestEmailSettingsRequiresPasswordOnFirstSave(t *testing.T) {
	svc := New(Deps{Email: &fakeEmail{}})
	err := svc.SaveEmailSettings(entity.EmailSender{
		Emailer: entity.Email{Addr: "me@example.com", Smtp: "smtp.example.com"},
	})
	if err == nil {
		t.Fatal("首次保存缺少授权码应返回错误")
	}
}

func TestSendEmailDelegates(t *testing.T) {
	mailer := &fakeEmail{}
	svc := New(Deps{Email: mailer})
	if err := svc.SendEmail(entity.EmailContent{Topic: "hi", Desc: "body"}); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("应发送 1 封邮件，实际 %d", len(mailer.sent))
	}
	if mailer.sent[0].Topic != "hi" || mailer.sent[0].Desc != "body" {
		t.Fatalf("邮件内容不匹配: %+v", mailer.sent[0])
	}
}
