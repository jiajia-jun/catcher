package entity

// EmailSMTPPort 是当前邮件实现使用的 SMTP 隐式 TLS 端口
const EmailSMTPPort = 465

type Email struct {
	Addr string
	Smtp string
}

type EmailSender struct {
	Emailer  Email
	PassWord string
}

type EmailContent struct {
	Topic string
	Desc  string
}
