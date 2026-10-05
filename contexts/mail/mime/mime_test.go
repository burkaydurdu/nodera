package mime_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	gomail "github.com/emersion/go-message/mail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/mime"
)

var at = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func compose(t *testing.T, in mime.ComposeInput) *gomail.Reader {
	t.Helper()
	raw, err := mime.Compose(in)
	require.NoError(t, err)
	reader, err := gomail.CreateReader(bytes.NewReader(raw))
	require.NoError(t, err)

	return reader
}

func TestCompose_HeadersAndBothBodies(t *testing.T) {
	r := compose(t, mime.ComposeInput{
		From:       mime.Address{Email: "no-reply@example.com", Name: "Acme"},
		To:         []mime.Address{{Email: "ana@example.net", Name: "Ana"}},
		Cc:         []mime.Address{{Email: "bob@example.net"}},
		ReplyTo:    &mime.Address{Email: "help@example.com"},
		Subject:    "Şifreni sıfırla",
		Text:       "Kod: 123456",
		HTML:       "<p>Kod: <b>123456</b></p>",
		Headers:    map[string]string{"X-Order": "42"},
		MessageID:  "abc@example.com",
		Date:       at,
		FeedbackID: "project:team:mail:nodera",
	})

	from, err := r.Header.AddressList("From")
	require.NoError(t, err)
	assert.Equal(t, "no-reply@example.com", from[0].Address)
	subject, _ := r.Header.Subject()
	assert.Equal(t, "Şifreni sıfırla", subject)
	id, _ := r.Header.MessageID()
	assert.Equal(t, "abc@example.com", id)
	assert.Equal(t, "42", r.Header.Get("X-Order"))
	assert.Equal(t, "project:team:mail:nodera", r.Header.Get("Feedback-ID"))
	assert.Empty(t, r.Header.Get("Bcc"))

	var bodies []string
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		b, _ := io.ReadAll(part.Body)
		bodies = append(bodies, string(b))
	}
	assert.Equal(t, []string{"Kod: 123456", "<p>Kod: <b>123456</b></p>"}, bodies)
}

func TestCompose_Attachments(t *testing.T) {
	r := compose(t, mime.ComposeInput{
		From:        mime.Address{Email: "no-reply@example.com"},
		To:          []mime.Address{{Email: "ana@example.net"}},
		Subject:     "Invoice",
		Text:        "Attached.",
		Attachments: []mime.Attachment{{Filename: "invoice.pdf", ContentType: "application/pdf", Content: []byte("%PDF-1.7")}},
		MessageID:   "abc@example.com",
		Date:        at,
	})

	var filename string
	var content []byte
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if h, ok := part.Header.(*gomail.AttachmentHeader); ok {
			filename, _ = h.Filename()
			content, _ = io.ReadAll(part.Body)
		}
	}
	assert.Equal(t, "invoice.pdf", filename)
	assert.Equal(t, "%PDF-1.7", string(content))
}

func TestRewrite_KeepsTheBodyAndFillsWhatIsMissing(t *testing.T) {
	raw := "From: Acme <no-reply@example.com>\r\nTo: ana@example.net\r\nBcc: hidden@example.net\r\nReturn-Path: <x@y>\r\nSubject: Hello\r\n\r\nBody line\r\n"

	out, parsed, err := mime.Rewrite([]byte(raw), mime.RewriteInput{MessageID: "gen@example.com", Date: at, FeedbackID: "f"})
	require.NoError(t, err)

	assert.Equal(t, "no-reply@example.com", parsed.From.Email)
	assert.Equal(t, "Acme", parsed.From.Name)
	assert.Equal(t, "Hello", parsed.Subject)
	assert.Equal(t, "gen@example.com", parsed.MessageID)
	assert.Equal(t, []string{"ana@example.net"}, parsed.To)
	assert.Empty(t, parsed.Cc)

	text := string(out)
	assert.Contains(t, text, "Message-Id: <gen@example.com>")
	assert.Contains(t, text, "Feedback-Id: f")
	assert.NotContains(t, strings.ToLower(text), "bcc:")
	assert.NotContains(t, strings.ToLower(text), "return-path:")
	assert.True(t, strings.HasSuffix(text, "\r\n\r\nBody line\r\n"))
}

func TestRewrite_KeepsTheClientsMessageID(t *testing.T) {
	raw := "From: no-reply@example.com\r\nMessage-ID: <mine@example.com>\r\nDate: Mon, 05 Oct 2026 10:00:00 +0000\r\nSubject: Hi\r\n\r\nx"

	_, parsed, err := mime.Rewrite([]byte(raw), mime.RewriteInput{MessageID: "gen@example.com", Date: at})
	require.NoError(t, err)
	assert.Equal(t, "mine@example.com", parsed.MessageID)
}

func TestRewrite_RefusesAMessageWithoutFrom(t *testing.T) {
	_, _, err := mime.Rewrite([]byte("Subject: Hi\r\n\r\nx"), mime.RewriteInput{MessageID: "gen@example.com", Date: at})
	assert.Error(t, err)
}

func TestReserved_AreTheHeadersNoderaSets(t *testing.T) {
	for _, name := range []string{"From", "to", "BCC", "DKIM-Signature", "Return-Path", "Content-Type", "Feedback-ID", "X-KumoRef"} {
		assert.Truef(t, mime.Reserved(name), name)
	}
}

func TestReserved_LeavesTheClientItsOwnHeadersAndMessageID(t *testing.T) {
	for _, name := range []string{"X-Order", "List-Unsubscribe", "Message-ID"} {
		assert.Falsef(t, mime.Reserved(name), name)
	}
}

func TestRewrite_DropsTheMTAsTraceHeader(t *testing.T) {
	raw := "X-KumoRef: eyJmb3JnZWQiOnRydWV9\r\nFrom: a@example.com\r\nSubject: Hi\r\n\r\nx"

	out, _, err := mime.Rewrite([]byte(raw), mime.RewriteInput{MessageID: "gen@example.com", Date: at})

	require.NoError(t, err)
	assert.NotContains(t, string(out), "X-Kumoref")
	assert.NotContains(t, string(out), "X-KumoRef")
}
