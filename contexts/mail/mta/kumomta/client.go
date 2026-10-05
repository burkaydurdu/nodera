// Package kumomta is the MTA port's adapter to KumoMTA (docs/adr/0009): it
// injects over KumoMTA's HTTP API and reads the log records KumoMTA posts
// back through its log hook.
package kumomta

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/piggystats/nodera/contexts/mail/mta"
	"github.com/piggystats/nodera/utils/str"
)

const defaultTimeout = 30 * time.Second

type Config struct {
	InjectionURL string
	Username     string
	Password     string
	Timeout      time.Duration
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	return &Client{cfg: cfg, http: &http.Client{Timeout: timeout}}
}

type injectRequest struct {
	EnvelopeSender  string            `json:"envelope_sender"`
	Content         string            `json:"content"`
	Recipients      []injectRecipient `json:"recipients"`
	TemplateDialect string            `json:"template_dialect"`
	TraceHeaders    traceHeaders      `json:"trace_headers"`
}

// injectRecipient's metadata becomes the message's `extra` meta, which the
// policy turns into the tenant, the signing key, the copy's own return path
// and the ids log records carry.
type injectRecipient struct {
	Email    string            `json:"email"`
	Metadata map[string]string `json:"metadata"`
}

// traceHeaders: sending the object at all turns KumoMTA's Received header on
// unless it is switched off explicitly. The supplemental header is what a
// feedback-loop report names the recipient's copy by.
type traceHeaders struct {
	ReceivedHeader     bool     `json:"received_header"`
	SupplementalHeader bool     `json:"supplemental_header"`
	IncludeMetaNames   []string `json:"include_meta_names"`
}

type injectResponse struct {
	SuccessCount     int      `json:"success_count"`
	FailCount        int      `json:"fail_count"`
	FailedRecipients []string `json:"failed_recipients"`
	Errors           []string `json:"errors"`
}

// Inject hands one Message to KumoMTA. The content is final: the dialect is
// static so nothing in a Team's email is ever read as a template. KumoMTA
// queues a copy per recipient and the policy gives each its own return path,
// so the request's sender is only the one KumoMTA requires.
func (c *Client) Inject(ctx context.Context, in *mta.Injection) (*mta.Result, error) {
	if len(in.Envelope.Recipients) == 0 {
		return &mta.Result{Failed: map[string]string{}}, nil
	}

	request := injectRequest{
		EnvelopeSender:  in.Envelope.Recipients[0].ReturnPath,
		Content:         string(in.Content),
		TemplateDialect: "Static",
		TraceHeaders:    traceHeaders{ReceivedHeader: false, SupplementalHeader: true, IncludeMetaNames: []string{"x_message_id", "tenant", "x_return_path"}},
	}
	for _, r := range in.Envelope.Recipients {
		request.Recipients = append(request.Recipients, injectRecipient{Email: r.Email, Metadata: map[string]string{
			"message_id":        in.MessageID.String(),
			"position":          strconv.Itoa(r.Position),
			"return_path":       r.ReturnPath,
			"tenant":            in.Envelope.ProjectID.String(),
			"team_id":           in.Envelope.TeamID.String(),
			"sending_domain_id": in.Envelope.SendingDomainID.String(),
			"dkim_domain":       in.Envelope.DKIMDomain,
		}})
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.InjectionURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if str.IsNotEmpty(c.cfg.Username) {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kumomta answered %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}

	var answer injectResponse
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("kumomta answer is not json: %w", err)
	}

	return &mta.Result{Failed: failures(answer)}, nil
}

// failures pairs each refused recipient with its reason; KumoMTA reports
// every error as "address: reason".
func failures(answer injectResponse) map[string]string {
	out := map[string]string{}
	for _, address := range answer.FailedRecipients {
		out[strings.ToLower(address)] = "refused by the MTA"
	}
	for _, e := range answer.Errors {
		if address, reason, ok := strings.Cut(e, ": "); ok {
			out[strings.ToLower(strings.TrimSpace(address))] = strings.TrimSpace(reason)
		}
	}

	return out
}
