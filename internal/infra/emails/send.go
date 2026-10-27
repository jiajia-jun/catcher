package emails

import (
	"bytes"
	"catcher/internal/domain/entity"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SendEmail 通过隐式 TLS（465 端口）给发件人自己发送一封邮件。
func (E *Email) SendEmail(desc *entity.EmailContent) error {
	if desc == nil {
		return fmt.Errorf("邮件内容为空")
	}
	if E.SenderAddr == "" || E.SMTP == "" {
		return fmt.Errorf("邮箱配置不完整，请先填写发件邮箱与 SMTP 服务器")
	}
	if E.Password == "" {
		return fmt.Errorf("缺少邮箱授权码，请先完成邮箱配置")
	}

	addr := net.JoinHostPort(E.SMTP, strconv.Itoa(entity.EmailSMTPPort))
	conn, err := tls.DialWithDialer(&net.Dialer{
		Timeout: 15 * time.Second,
	}, "tcp", addr, &tls.Config{
		ServerName: E.SMTP,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return fmt.Errorf("连接邮件服务器失败: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	c, err := smtp.NewClient(conn, E.SMTP)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("创建 SMTP 客户端失败: %w", err)
	}
	defer c.Close()

	auth := smtp.PlainAuth("", E.SenderAddr, E.Password, E.SMTP)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("邮箱认证失败，请检查授权码: %w", err)
	}
	if err := c.Mail(E.SenderAddr); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	if err := c.Rcpt(E.SenderAddr); err != nil {
		return fmt.Errorf("设置收件人失败: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("开始写入邮件失败: %w", err)
	}
	data, err := E.buildMessage(desc)
	if err != nil {
		_ = w.Close()
		return err
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件失败: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("结束 SMTP 会话失败: %w", err)
	}
	return nil
}

func (E *Email) buildMessage(desc *entity.EmailContent) ([]byte, error) {
	topic := strings.NewReplacer("\r", " ", "\n", " ").Replace(desc.Topic)
	topic = strings.TrimSpace(topic)

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", E.SenderAddr)
	fmt.Fprintf(&b, "To: %s\r\n", E.SenderAddr)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", topic))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(desc.Desc)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	b.WriteString("\r\n")
	return b.Bytes(), nil
}
