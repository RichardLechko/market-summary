package digest

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Mail holds SMTP settings.
type Mail struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	To       []string
}

// Validate reports missing settings.
func (m Mail) Validate() error {
	var missing []string
	if m.User == "" {
		missing = append(missing, "SMTP_USER")
	}
	if m.Password == "" {
		missing = append(missing, "SMTP_PASSWORD")
	}
	if len(m.To) == 0 {
		missing = append(missing, "MAIL_TO")
	}
	if len(missing) > 0 {
		return fmt.Errorf("email not configured: set %s in .env", strings.Join(missing, ", "))
	}
	return nil
}

// Message builds a MIME message with an HTML body.
func Message(from string, to []string, subject, html string, now time.Time) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&buf, "Date: %s\r\n", now.Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	w := quotedprintable.NewWriter(&buf)
	if _, err := w.Write([]byte(html)); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Send delivers the HTML digest. net/smtp upgrades to STARTTLS when the
// server offers it (Gmail on port 587 does) and refuses PLAIN auth without TLS.
func (m Mail) Send(subject, html string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.From == "" {
		return errors.New("MAIL_FROM or SMTP_USER must be set")
	}
	msg, err := Message(m.From, m.To, subject, html, time.Now())
	if err != nil {
		return err
	}
	auth := smtp.PlainAuth("", m.User, m.Password, m.Host)
	return smtp.SendMail(net.JoinHostPort(m.Host, m.Port), auth, m.From, m.To, msg)
}
