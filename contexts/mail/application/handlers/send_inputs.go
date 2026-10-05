package handlers

import (
	"context"
	"encoding/base64"
	"slices"
	"strings"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/apikey"
	"github.com/piggystats/nodera/contexts/mail/application/dto"
	"github.com/piggystats/nodera/contexts/mail/mime"
	"github.com/piggystats/nodera/utils/str"
)

// SendHTTP turns the sending API's body into a Message: the bodies,
// attachments and headers are checked here, before anything is counted.
func (h *Sender) SendHTTP(ctx context.Context, key *apikey.Key, req *message.SendRequest) (*dto.Sent, error) {
	if str.IsBlank(req.Text) && str.IsBlank(req.HTML) {
		return nil, message.ErrorNoContent
	}
	if strings.ContainsAny(req.Subject, "\r\n") {
		return nil, message.ErrorMalformedMessage.Copy().SetMessage("the subject holds a line break")
	}
	if err := checkHeaders(req.Headers); err != nil {
		return nil, err
	}
	clientMessageID, headers, err := takeMessageID(req.Headers)
	if err != nil {
		return nil, err
	}

	recipients, err := message.NormalizeRecipients(addresses(req.To), addresses(req.Cc), addresses(req.Bcc))
	if err != nil {
		return nil, err
	}

	attachments, size, err := decodeAttachments(req.Attachments)
	if err != nil {
		return nil, err
	}
	size += len(req.Text) + len(req.HTML)
	if size > message.MaxSizeBytes {
		return nil, message.ErrorTooLarge
	}

	var replyTo *mime.Address
	if req.ReplyTo != nil {
		address, err := message.NormalizeAddress(req.ReplyTo.Email)
		if err != nil {
			return nil, err
		}
		replyTo = &mime.Address{Email: address, Name: req.ReplyTo.Name}
	}

	content := func(c ContentContext) ([]byte, string, error) {
		messageID := c.MessageIDHeader
		if str.IsNotEmpty(clientMessageID) {
			messageID = clientMessageID
		}
		raw, err := mime.Compose(mime.ComposeInput{
			From: mime.Address{Email: strings.ToLower(strings.TrimSpace(req.From.Email)), Name: req.From.Name},
			To:   byKind(recipients, message.KindTo), Cc: byKind(recipients, message.KindCc), ReplyTo: replyTo,
			Subject: req.Subject, Text: req.Text, HTML: req.HTML, Attachments: attachments, Headers: headers,
			MessageID: messageID, Date: c.Date, FeedbackID: c.FeedbackID,
		})

		return raw, messageID, err
	}

	m, err := h.Send(ctx, SendInput{
		Key: key, Source: message.SourceAPI, From: message.Address{Email: req.From.Email, Name: req.From.Name},
		Recipients: recipients, Subject: req.Subject, Tags: req.Tags, IdempotencyKey: req.IdempotencyKey, Content: content,
	})
	if err != nil {
		return nil, err
	}

	return dto.ToSent(m), nil
}

// SendSMTP turns what an SMTP client sent into a Message. The envelope's
// recipients are the recipients; one in neither To nor Cc was a Bcc.
func (h *Sender) SendSMTP(ctx context.Context, key *apikey.Key, envelopeRecipients []string, raw []byte) (*message.Message, error) {
	_, parsed, err := mime.Rewrite(raw, mime.RewriteInput{MessageID: "probe@nodera.invalid", Date: h.now()})
	if err != nil {
		return nil, message.ErrorMalformedMessage.Copy().SetError(err)
	}

	var to, cc, bcc []message.Address
	for _, rcpt := range envelopeRecipients {
		address := strings.ToLower(strings.TrimSpace(rcpt))
		switch {
		case slices.Contains(parsed.To, address):
			to = append(to, message.Address{Email: address})
		case slices.Contains(parsed.Cc, address):
			cc = append(cc, message.Address{Email: address})
		default:
			bcc = append(bcc, message.Address{Email: address})
		}
	}
	recipients, err := message.NormalizeRecipients(to, cc, bcc)
	if err != nil {
		return nil, err
	}

	content := func(c ContentContext) ([]byte, string, error) {
		out, final, err := mime.Rewrite(raw, mime.RewriteInput{MessageID: c.MessageIDHeader, Date: c.Date, FeedbackID: c.FeedbackID})
		if err != nil {
			return nil, "", err
		}

		return out, final.MessageID, nil
	}

	return h.Send(ctx, SendInput{
		Key: key, Source: message.SourceSMTP, From: message.Address{Email: parsed.From.Email, Name: parsed.From.Name},
		Recipients: recipients, Subject: parsed.Subject, Content: content,
	})
}

// checkHeaders keeps a Team from choosing a header Nodera sets, and from
// smuggling a second header in through a line break.
func checkHeaders(headers map[string]string) error {
	for name, value := range headers {
		if mime.Reserved(name) {
			return message.ErrorReservedHeader.Copy().SetMessagef("the %s header is set by Nodera", name)
		}
		if !fieldName(name) || strings.ContainsAny(value, "\r\n") {
			return message.ErrorMalformedMessage.Copy().SetMessagef("invalid header %q", name)
		}
	}

	return nil
}

// maxMessageIDLength keeps a client's Message-ID within one header line.
const maxMessageIDLength = 250

// takeMessageID answers the Message-ID a client chose, without its angle
// brackets, and the other headers; Nodera writes the one it keeps.
func takeMessageID(headers map[string]string) (string, map[string]string, error) {
	rest := make(map[string]string, len(headers))
	messageID := ""
	for name, value := range headers {
		if !strings.EqualFold(name, "Message-ID") {
			rest[name] = value

			continue
		}
		messageID = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "<"), ">")
		local, host, found := strings.Cut(messageID, "@")
		if !found || str.IsEmpty(local) || str.IsEmpty(host) || strings.Contains(host, "@") ||
			strings.ContainsAny(messageID, "<> \t") || len(messageID) > maxMessageIDLength {
			return "", nil, message.ErrorMalformedMessage.Copy().SetMessage("the Message-ID header is not <id@host>")
		}
	}

	return messageID, rest, nil
}

// fieldName is RFC 5322's: printable ASCII without the colon.
func fieldName(name string) bool {
	if str.IsEmpty(name) {
		return false
	}
	for _, r := range name {
		if r < 33 || r > 126 || r == ':' {
			return false
		}
	}

	return true
}

func decodeAttachments(in []message.AttachmentInput) ([]mime.Attachment, int, error) {
	out := make([]mime.Attachment, 0, len(in))
	size := 0
	for _, a := range in {
		content, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return nil, 0, message.ErrorInvalidAttachment
		}
		if strings.ContainsAny(a.Filename+a.ContentType+a.ContentID, "\r\n") {
			return nil, 0, message.ErrorInvalidAttachment
		}
		size += len(content)
		out = append(out, mime.Attachment{Filename: a.Filename, ContentType: a.ContentType, Content: content, ContentID: a.ContentID})
	}

	return out, size, nil
}

func addresses(in []message.AddressInput) []message.Address {
	out := make([]message.Address, 0, len(in))
	for _, a := range in {
		out = append(out, message.Address(a))
	}

	return out
}

func byKind(recipients []*message.Recipient, kind message.Kind) []mime.Address {
	var out []mime.Address
	for _, r := range recipients {
		if r.Kind == kind {
			out = append(out, mime.Address{Email: r.Address, Name: r.Name})
		}
	}

	return out
}
