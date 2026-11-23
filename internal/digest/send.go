package digest

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
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

// Attachment is a file attached to the email.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
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

// Message builds a MIME message with an HTML body and optional attachments.
func Message(from string, to []string, subject, html string, now time.Time, atts ...Attachment) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&buf, "Date: %s\r\n", now.Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if len(atts) == 0 {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		if err := writeQP(&buf, html); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}

	mw := multipart.NewWriter(&buf)
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mw.Boundary())

	body, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/html; charset=UTF-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	var qp bytes.Buffer
	if err := writeQP(&qp, html); err != nil {
		return nil, err
	}
	if _, err := body.Write(qp.Bytes()); err != nil {
		return nil, err
	}

	for _, a := range atts {
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {fmt.Sprintf("%s; name=%q", a.ContentType, a.Name)},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {fmt.Sprintf("attachment; filename=%q", a.Name)},
		})
		if err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for len(enc) > 76 {
			if _, err := part.Write([]byte(enc[:76] + "\r\n")); err != nil {
				return nil, err
			}
			enc = enc[76:]
		}
		if _, err := part.Write([]byte(enc + "\r\n")); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeQP(buf *bytes.Buffer, s string) error {
	w := quotedprintable.NewWriter(buf)
	if _, err := w.Write([]byte(s)); err != nil {
		return err
	}
	return w.Close()
}

// Send delivers the email. net/smtp upgrades to STARTTLS when the server
// offers it (Gmail on port 587 does) and refuses PLAIN auth without TLS.
func (m Mail) Send(subject, html string, atts ...Attachment) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.From == "" {
		return errors.New("MAIL_FROM or SMTP_USER must be set")
	}
	msg, err := Message(m.From, m.To, subject, html, time.Now(), atts...)
	if err != nil {
		return err
	}
	auth := smtp.PlainAuth("", m.User, m.Password, m.Host)
	return smtp.SendMail(net.JoinHostPort(m.Host, m.Port), auth, m.From, m.To, msg)
}
