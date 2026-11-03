// Package mail sends plain-text email through an SMTP server.
package mail

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Security is how the connection to the SMTP server is protected.
type Security string

const (
	// StartTLS connects in plain text and upgrades; TLS is required (port 587).
	StartTLS Security = "starttls"
	// TLS connects with TLS from the start (port 465).
	TLS Security = "tls"
	// None sends in plain text. Only for a relay on the same host or network.
	None Security = "none"
)

// Server is where and how to send.
type Server struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "Name <address>" or an address
	Security Security
	// InsecureSkipVerify is for tests only.
	InsecureSkipVerify bool
}

// Validate checks the settings without connecting.
func (s Server) Validate() map[string]string {
	errs := map[string]string{}
	if s.Host == "" || strings.ContainsAny(s.Host, " /\r\n") {
		errs["smtpHost"] = "Enter the mail server's host name."
	}
	if s.Port < 1 || s.Port > 65535 {
		errs["smtpPort"] = "Use a port from 1 to 65535 (usually 587 or 465)."
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		errs["smtpFrom"] = `Use an address like "Flagfall CTF <ctf@example.org>".`
	}
	switch s.Security {
	case StartTLS, TLS, None:
	default:
		errs["smtpSecurity"] = "Use starttls, tls or none."
	}
	return errs
}

var errHeader = errors.New("line breaks aren't allowed in email headers")

// Message builds an RFC 5322 message with a quoted-printable UTF-8 body.
func Message(from, to, subject, body string, now time.Time) ([]byte, error) {
	for _, h := range []string{from, to, subject} {
		if strings.ContainsAny(h, "\r\n") {
			return nil, errHeader
		}
	}
	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("from address: %w", err)
	}
	toAddr, err := mail.ParseAddress(to)
	if err != nil {
		return nil, fmt.Errorf("to address: %w", err)
	}
	idBytes := make([]byte, 12)
	_, _ = rand.Read(idBytes)
	domain := fromAddr.Address[strings.LastIndex(fromAddr.Address, "@")+1:]

	var b bytes.Buffer
	header := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	header("From", fromAddr.String())
	header("To", toAddr.String())
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+base64.RawURLEncoding.EncodeToString(idBytes)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes(), nil
}

// Send delivers one message. It gives up after the context's deadline, or
// 30 seconds if there is none.
func Send(ctx context.Context, s Server, to, subject, body string) error {
	if errs := s.Validate(); len(errs) > 0 {
		return errors.New("the mail settings are incomplete")
	}
	msg, err := Message(s.From, to, subject, body, time.Now())
	if err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	tlsConf := &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: s.InsecureSkipVerify} //nolint:gosec // tests only

	var conn net.Conn
	if s.Security == TLS {
		conn, err = (&tls.Dialer{Config: tlsConf}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("talk to %s: %w", addr, err)
	}
	defer c.Close()
	if s.Security == StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s doesn't offer STARTTLS; choose another security setting", addr)
		}
		if err := c.StartTLS(tlsConf); err != nil {
			return fmt.Errorf("start TLS: %w", err)
		}
	}
	if s.Username != "" {
		// PlainAuth refuses to send the password over a plain connection,
		// except to localhost.
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("sign in to the mail server: %w", err)
		}
	}
	fromAddr, _ := mail.ParseAddress(s.From)
	toAddr, _ := mail.ParseAddress(to)
	if err := c.Mail(fromAddr.Address); err != nil {
		return fmt.Errorf("mail server refused the sender: %w", err)
	}
	if err := c.Rcpt(toAddr.Address); err != nil {
		return fmt.Errorf("mail server refused the recipient: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail server refused the message: %w", err)
	}
	return c.Quit()
}

// Seal encrypts a secret (the SMTP password) for storage, with a key
// derived from the install's secret key.
func Seal(secretKey []byte, plain string) (string, error) {
	gcm, err := aead(secretKey)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// ErrUnreadable means a sealed value was made with another secret key,
// e.g. after restoring a backup on a different server.
var ErrUnreadable = errors.New("the stored password can't be decrypted with this server's secret key; enter it again")

// Open decrypts a value from Seal.
func Open(secretKey []byte, sealed string) (string, error) {
	gcm, err := aead(secretKey)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(sealed, "v1:"))
	if err != nil || !strings.HasPrefix(sealed, "v1:") || len(raw) < gcm.NonceSize() {
		return "", ErrUnreadable
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", ErrUnreadable
	}
	return string(plain), nil
}

func aead(secretKey []byte) (cipher.AEAD, error) {
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte("flagfall/mail-password/v1"))
	block, err := aes.NewCipher(mac.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
