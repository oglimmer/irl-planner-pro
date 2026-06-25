// Package email sends plain-text notifications over SMTP using only the
// standard library. It is intentionally minimal: a single Sender value carries
// the connection settings and Send delivers one message to a set of
// recipients. There is no templating, queueing, or retry — callers that need
// those build them on top.
package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// Sender holds SMTP connection settings. The zero value is "not configured":
// Configured reports false until at least a host and From address are set.
type Sender struct {
	Host     string // SMTP server host (no port)
	Port     int    // SMTP server port, e.g. 587 (STARTTLS) or 465/25
	Username string // SMTP auth username; empty disables AUTH
	Password string // SMTP auth password
	From     string // envelope + header From address
	UseTLS   bool   // when true, upgrade the connection with STARTTLS
}

// Configured reports whether enough settings are present to attempt delivery.
func (s Sender) Configured() bool {
	return strings.TrimSpace(s.Host) != "" && strings.TrimSpace(s.From) != ""
}

// Send delivers a single plain-text message to every address in to. It returns
// an error if the sender is unconfigured, has no recipients, or delivery fails.
func (s Sender) Send(to []string, subject, body string) error {
	if !s.Configured() {
		return fmt.Errorf("email: sender not configured")
	}
	recipients := make([]string, 0, len(to))
	for _, r := range to {
		if r = strings.TrimSpace(r); r != "" {
			recipients = append(recipients, r)
		}
	}
	if len(recipients) == 0 {
		return fmt.Errorf("email: no recipients")
	}

	msg := buildMessage(s.From, recipients, subject, body)
	addr := net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))

	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}
	defer c.Close()

	if err := c.Hello(clientHostname()); err != nil {
		return fmt.Errorf("email: hello: %w", err)
	}

	if s.UseTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				return fmt.Errorf("email: starttls: %w", err)
			}
		} else {
			return fmt.Errorf("email: STARTTLS requested but server does not support it")
		}
	}

	if s.Username != "" {
		auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}

	if err := c.Mail(s.From); err != nil {
		return fmt.Errorf("email: mail from: %w", err)
	}
	for _, r := range recipients {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("email: rcpt %s: %w", r, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: data: %w", err)
	}
	if _, err := wc.Write(msg); err != nil {
		wc.Close()
		return fmt.Errorf("email: write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("email: close data: %w", err)
	}
	return c.Quit()
}

// buildMessage assembles a minimal RFC 5322 plain-text message. CRLF line
// endings are used as required by SMTP. The Date header is omitted so the
// package stays free of non-deterministic clock reads for testability; most
// MTAs add one on receipt.
func buildMessage(from string, to []string, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	// Normalize body line endings to CRLF and dot-stuff leading dots.
	body = strings.ReplaceAll(body, "\r\n", "\n")
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, ".") {
			line = "." + line
		}
		b.WriteString(line)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// clientHostname returns a HELO/EHLO name. It avoids os.Hostname failures by
// falling back to "localhost", which all MTAs accept.
func clientHostname() string {
	return "localhost"
}
