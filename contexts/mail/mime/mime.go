// Package mime builds the Messages Nodera hands to its MTA: composed from the
// HTTP API's fields, or rewritten from what an SMTP client sent.
package mime

import (
	"bufio"
	"bytes"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-message"
	gomail "github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"

	"github.com/piggystats/nodera/utils/str"
)

// reserved headers are set by Nodera, or by the MTA after it, and a client
// may not choose them. A Message-ID is the client's to choose.
var reserved = []string{
	"from", "to", "cc", "bcc", "subject", "date", "reply-to", "return-path", "sender",
	"mime-version", "content-type", "content-transfer-encoding", "content-disposition", "dkim-signature",
	"received", "feedback-id", "x-kumoref",
}

func Reserved(name string) bool {
	return slices.Contains(reserved, strings.ToLower(strings.TrimSpace(name)))
}

type Address struct {
	Email string
	Name  string
}

type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
	// ContentID makes the part inline, for an HTML body to reference.
	ContentID string
}

type ComposeInput struct {
	From        Address
	To          []Address
	Cc          []Address
	ReplyTo     *Address
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
	Headers     map[string]string
	MessageID   string
	Date        time.Time
	FeedbackID  string
}

// Compose builds the RFC 5322 Message. Bcc recipients are only on the
// envelope, never in a header.
func Compose(in ComposeInput) ([]byte, error) {
	var h gomail.Header
	h.SetDate(in.Date)
	h.SetAddressList("From", []*gomail.Address{address(in.From)})
	if len(in.To) > 0 {
		h.SetAddressList("To", addresses(in.To))
	}
	if len(in.Cc) > 0 {
		h.SetAddressList("Cc", addresses(in.Cc))
	}
	if in.ReplyTo != nil {
		h.SetAddressList("Reply-To", []*gomail.Address{address(*in.ReplyTo)})
	}
	h.SetSubject(in.Subject)
	h.SetMessageID(in.MessageID)
	if str.IsNotEmpty(in.FeedbackID) {
		h.Set("Feedback-ID", in.FeedbackID)
	}
	for name, value := range in.Headers {
		h.Set(name, value)
	}

	var buf bytes.Buffer
	w, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	if err := writeBodies(w, in.Text, in.HTML); err != nil {
		return nil, err
	}
	for _, a := range in.Attachments {
		if err := writeAttachment(w, a); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func writeBodies(w *gomail.Writer, text, html string) error {
	alternative, err := w.CreateInline()
	if err != nil {
		return err
	}

	for _, body := range []struct{ mediaType, content string }{{"text/plain", text}, {"text/html", html}} {
		if str.IsEmpty(body.content) {
			continue
		}
		var ih gomail.InlineHeader
		ih.SetContentType(body.mediaType, map[string]string{"charset": "utf-8"})
		part, err := alternative.CreatePart(ih)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(part, body.content); err != nil {
			return err
		}
		if err := part.Close(); err != nil {
			return err
		}
	}

	return alternative.Close()
}

func writeAttachment(w *gomail.Writer, a Attachment) error {
	var ah gomail.AttachmentHeader
	ah.SetContentType(a.ContentType, nil)
	ah.SetFilename(a.Filename)
	if str.IsNotEmpty(a.ContentID) {
		ah.Set("Content-ID", "<"+strings.Trim(a.ContentID, "<>")+">")
		ah.Set("Content-Disposition", "inline; filename=\""+a.Filename+"\"")
	}

	part, err := w.CreateAttachment(ah)
	if err != nil {
		return err
	}
	if _, err := part.Write(a.Content); err != nil {
		return err
	}

	return part.Close()
}

type RewriteInput struct {
	// MessageID and Date are used only when the client set none.
	MessageID  string
	Date       time.Time
	FeedbackID string
}

// Parsed is what Nodera reads from an SMTP client's Message.
type Parsed struct {
	From      Address
	Subject   string
	MessageID string
	// To and Cc are the header's addresses in lower case; an envelope
	// recipient in neither was a Bcc.
	To []string
	Cc []string
}

// Rewrite keeps the client's body byte for byte and fixes the header: a
// Message-ID and a Date when missing, Nodera's Feedback-ID, no Bcc or
// Return-Path, which the envelope owns, and no trace header of the MTA's,
// which a feedback report would otherwise read as Nodera's.
func Rewrite(raw []byte, in RewriteInput) ([]byte, *Parsed, error) {
	reader := bufio.NewReader(bytes.NewReader(raw))
	th, err := textproto.ReadHeader(reader)
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, err
	}

	h := gomail.Header{Header: message.Header{Header: th}}
	from, err := h.AddressList("From")
	if err != nil || len(from) == 0 {
		return nil, nil, ErrNoFrom
	}

	parsed := &Parsed{From: Address{Email: from[0].Address, Name: from[0].Name}}
	parsed.Subject, _ = h.Subject()
	parsed.To = lowerAddresses(h, "To")
	parsed.Cc = lowerAddresses(h, "Cc")

	parsed.MessageID, _ = h.MessageID()
	if str.IsEmpty(parsed.MessageID) {
		h.SetMessageID(in.MessageID)
		parsed.MessageID = in.MessageID
	}
	if !h.Has("Date") {
		h.SetDate(in.Date)
	}
	h.Del("Bcc")
	h.Del("Return-Path")
	h.Del("X-KumoRef")
	if str.IsNotEmpty(in.FeedbackID) {
		h.Set("Feedback-ID", in.FeedbackID)
	}

	var out bytes.Buffer
	if err := textproto.WriteHeader(&out, h.Header.Header); err != nil {
		return nil, nil, err
	}
	out.Write(body)

	return out.Bytes(), parsed, nil
}

func lowerAddresses(h gomail.Header, field string) []string {
	list, _ := h.AddressList(field)
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}

	return out
}

func address(a Address) *gomail.Address {
	return &gomail.Address{Name: a.Name, Address: a.Email}
}

func addresses(list []Address) []*gomail.Address {
	out := make([]*gomail.Address, len(list))
	for i, a := range list {
		out[i] = address(a)
	}

	return out
}
