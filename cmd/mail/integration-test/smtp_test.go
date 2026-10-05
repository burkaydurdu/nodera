package integrationtest

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/smtp"
	"net/textproto"
	"strings"
)

// smtpClient connects, upgrades to TLS and authenticates the way a ready-made
// application does.
func (s *mailSuite) smtpClient(auth smtp.Auth) (*smtp.Client, error) {
	c, err := smtp.Dial(s.smtpAddr)
	if err != nil {
		return nil, err
	}
	if err := c.StartTLS(&tls.Config{InsecureSkipVerify: true, ServerName: "localhost"}); err != nil { //nolint:gosec // a throwaway test certificate
		return nil, err
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			_ = c.Close()

			return nil, err
		}
	}

	return c, nil
}

func (s *mailSuite) smtpSend(key string, rcpts []string, raw string) error {
	c, err := s.smtpClient(smtp.PlainAuth("", "apikey", key, s.smtpHost()))
	if err != nil {
		return err
	}
	defer func() { _ = c.Quit() }()

	if err := c.Mail("whatever@client.example"); err != nil {
		return err
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(raw)); err != nil {
		return err
	}

	return w.Close()
}

// smtpHost is the name net/smtp checks PLAIN credentials against.
func (s *mailSuite) smtpHost() string {
	host, _, _ := strings.Cut(s.smtpAddr, ":")

	return host
}

func code(err error) int {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return tp.Code
	}

	return 0
}

const smtpMessage = "From: Shop <no-reply@example.com>\r\nTo: ana@example.net\r\nBcc: hidden@example.net\r\nSubject: Welcome\r\n\r\nHello from WordPress\r\n"

func (s *mailSuite) TestSMTP_SendsWithTheAPIKeyAsPassword() {
	p, key := s.ready()

	s.Require().NoError(s.smtpSend(key, []string{"ana@example.net", "hidden@example.net"}, smtpMessage))

	logs := s.manage(http.MethodGet, projectPath(p, "/messages"), "", p.Owner)
	s.Require().Len(logs.items(), 1)
	entry := logs.items()[0]
	s.Equal("smtp", entry["source"])
	s.Equal("Welcome", entry["subject"])

	detail := s.manage(http.MethodGet, projectPath(p, "/messages/"+entry["id"].(string)), "", p.Owner)
	recipients := detail.Body["recipients"].([]any)
	s.Equal("to", recipients[0].(map[string]any)["kind"])
	s.Equal("bcc", recipients[1].(map[string]any)["kind"], "an envelope recipient outside To and Cc was a Bcc")

	raw := string(s.mta.Injections()[0].Content)
	s.NotContains(strings.ToLower(raw), "bcc:")
	s.Contains(raw, "Feedback-Id: ")
	s.Contains(raw, "Hello from WordPress")
}

// The MTA's trace header names the copy a feedback report is about; one a
// client wrote would be read as Nodera's.
func (s *mailSuite) TestSMTP_DropsTheHeadersTheEnvelopeAndTheMTAOwn() {
	_, key := s.ready()
	raw := "X-KumoRef: eyJmb3JnZWQiOnRydWV9\r\nReturn-Path: <bounces@client.example>\r\n" + smtpMessage

	s.Require().NoError(s.smtpSend(key, []string{"ana@example.net"}, raw))

	content := strings.ToLower(string(s.mta.Injections()[0].Content))
	s.NotContains(content, "x-kumoref:")
	s.NotContains(content, "return-path:")
}

func (s *mailSuite) TestSMTP_LoginMechanismToo() {
	_, key := s.ready()

	c, err := s.smtpClient(&loginAuth{user: "apikey", password: key})
	s.Require().NoError(err)
	_ = c.Quit()
}

func (s *mailSuite) TestSMTP_CredentialsNeedTLSAndAValidKey() {
	_, key := s.ready()

	plain, err := smtp.Dial(s.smtpAddr)
	s.Require().NoError(err)
	err = plain.Auth(&loginAuth{user: "apikey", password: key})
	s.Error(err, "no AUTH before STARTTLS")
	_ = plain.Close()

	_, err = s.smtpClient(smtp.PlainAuth("", "apikey", key+"x", s.smtpHost()))
	s.Equal(535, code(err))
	_, err = s.smtpClient(smtp.PlainAuth("", "someone", key, s.smtpHost()))
	s.Equal(535, code(err))

	c, err := s.smtpClient(nil)
	s.Require().NoError(err)
	refused := c.Mail("x@example.com")
	s.Equal(502, code(refused), "nothing is accepted before AUTH")
	s.ErrorContains(refused, "authenticate")
	_ = c.Close()
}

func (s *mailSuite) TestSMTP_TemporaryRefusalsAre4xxAndPermanentOnes5xx() {
	p := s.grantProject()
	s.verifiedDomain(p, "example.com")
	key := s.apiKey(p)

	s.Equal(550, code(s.smtpSend(key, []string{"ana@example.net"}, smtpMessage)), "no Package needs the Team")

	s.Require().Equal(200, s.choose(p, "10k").Code)
	s.exec(`UPDATE project_limits SET daily_limit = 1 WHERE project_id = $1`, p.ProjectID)
	s.Require().NoError(s.smtpSend(key, []string{"ana@example.net"}, smtpMessage))
	s.Equal(452, code(s.smtpSend(key, []string{"bob@example.net"}, smtpMessage)), "the Daily limit clears at midnight")

	s.Equal(550, code(s.smtpSend(key, []string{"ana@example.net"}, strings.Replace(smtpMessage, "no-reply@example.com", "no-reply@other.org", 1))))
}

// loginAuth is the LOGIN mechanism net/smtp does not ship.
type loginAuth struct {
	user, password string
}

func (a *loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	if strings.Contains(strings.ToLower(string(fromServer)), "username") {
		return []byte(a.user), nil
	}

	return []byte(a.password), nil
}
