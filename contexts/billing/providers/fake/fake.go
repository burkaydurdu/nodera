// Package fake is an in-memory Payment provider scripted by tests: the next
// verification's outcome and card, the next charges' outcomes, payment states
// for reconciliation, 3-D Secure callbacks and webhooks signed with a shared
// secret. Like Craftgate it does not dedupe charges by idempotency key, so a
// suite that charges an attempt twice sees two captures.
package fake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/utils/str"
)

const (
	// DefaultCode stands in for the seeded routing rows, so the seed applies.
	DefaultCode = "craftgate"
	// SignatureHeader carries the hex HMAC-SHA256 of the body.
	SignatureHeader = "X-Fake-Signature"
	// SigningKey is what the fake signs and verifies webhooks and 3-D Secure
	// callbacks with.
	SigningKey   = "fake-webhook-signing-key"
	redirectBase = "https://hosted.payments.test/verify/"
	confirmBase  = "https://hosted.payments.test/3ds/"
	threeDSKey   = ":3ds"

	// CallbackPaymentID, CallbackStatus and CallbackHash are the fields the
	// fake's 3-D Secure callback posts; StatusSuccess is a passed check.
	CallbackPaymentID = "paymentId"
	CallbackStatus    = "status"
	CallbackHash      = "hash"
	StatusSuccess     = "SUCCESS"
	StatusFailure     = "FAILURE"
)

// ErrTimeout is what a provider that never answered looks like.
var ErrTimeout = fmt.Errorf("fake provider: %w", context.DeadlineExceeded)

type verification struct {
	req      providers.VerificationRequest
	finished bool
}

// payment is the provider's own record: what a fetch or a search answers.
type payment struct {
	ref   string
	key   string
	state providers.PaymentState
}

type chargeScript struct {
	result  providers.ChargeResult
	err     error
	capture bool
}

// Fake records every call so a test can assert what the provider was asked.
type Fake struct {
	mu   sync.Mutex
	code string

	verifications map[string]*verification
	nextID        int

	// Scripted outcomes, consumed in order; the default is success.
	verifyOutcomes []string
	nextCards      []providers.SavedCard
	charges        []chargeScript
	completions    []providers.ChargeResult
	cardStatuses   map[string]providers.CardStatus
	confirmPages   []providers.Confirmation
	hold           *hold

	payments map[string]*payment
	order    []string

	failStart error

	Verifications []providers.VerificationRequest
	Charges       []providers.ChargeRequest
	Confirmations []providers.ChargeRequest
	Refunds       []providers.RefundRequest
}

type hold struct {
	entered chan struct{}
	release chan struct{}
}

func New(code string) *Fake {
	if str.IsBlank(code) {
		code = DefaultCode
	}
	f := &Fake{code: code}
	f.Reset()

	return f
}

// Reset forgets every call and script.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifications = make(map[string]*verification)
	f.payments = make(map[string]*payment)
	f.cardStatuses = make(map[string]providers.CardStatus)
	f.order = nil
	f.verifyOutcomes, f.nextCards, f.charges, f.completions, f.confirmPages = nil, nil, nil, nil, nil
	f.failStart, f.hold = nil, nil
	f.Verifications, f.Charges, f.Confirmations, f.Refunds = nil, nil, nil, nil
}

// ---------------------------------------------------------------------------
// Scripting
// ---------------------------------------------------------------------------

// FailNextVerification makes the next completion a decline with the reason.
func (f *Fake) FailNextVerification(reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifyOutcomes = append(f.verifyOutcomes, reason)
}

// NextCard sets the card the next successful verification saves.
func (f *Fake) NextCard(brand, last4 string, expMonth, expYear int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextCards = append(f.nextCards, providers.SavedCard{Brand: brand, Last4: last4, ExpMonth: expMonth, ExpYear: expYear})
}

// FailStart makes StartCardVerification answer err, as an unreachable provider.
func (f *Fake) FailStart(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failStart = err
}

// NextChargeSucceeds, NextChargeFails, NextChargeNeedsConfirmation and
// NextChargePending queue the outcome of the next Charge.
func (f *Fake) NextChargeSucceeds() {
	f.queueCharge(chargeScript{result: providers.ChargeResult{State: providers.StateSucceeded}, capture: true})
}

func (f *Fake) NextChargeFails(reason string) {
	f.queueCharge(chargeScript{result: providers.ChargeResult{State: providers.StateFailed, FailureReason: reason}})
}

// NextChargeNeedsConfirmation is a bank that refuses the charge without the
// cardholder: nothing is started until the cardholder opens the link.
func (f *Fake) NextChargeNeedsConfirmation() {
	f.queueCharge(chargeScript{result: providers.ChargeResult{State: providers.StateConfirmationRequired}})
}

// NextChargePending answers the next Charge as accepted but not settled, as
// a provider that reports the outcome later by webhook.
func (f *Fake) NextChargePending() {
	f.queueCharge(chargeScript{result: providers.ChargeResult{State: providers.StatePending}})
}

// FailNextCharge makes the next Charge answer err without reaching the
// card, as a provider that refused the call.
func (f *Fake) FailNextCharge(err error) {
	f.queueCharge(chargeScript{err: err})
}

// NextChargeCapturesThenTimesOut captures the next Charge and loses the
// answer: the money moved, the caller saw a timeout.
func (f *Fake) NextChargeCapturesThenTimesOut() {
	f.queueCharge(chargeScript{result: providers.ChargeResult{State: providers.StateSucceeded}, err: ErrTimeout, capture: true})
}

func (f *Fake) queueCharge(s chargeScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.charges = append(f.charges, s)
}

// HoldNextCharge blocks the next Charge until release is called; entered is
// closed once the call is inside the provider.
func (f *Fake) HoldNextCharge() (entered <-chan struct{}, release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := &hold{entered: make(chan struct{}), release: make(chan struct{})}
	f.hold = h
	var once sync.Once

	return h.entered, func() { once.Do(func() { close(h.release) }) }
}

// NextConfirmationPage makes the next 3-D Secure step answer this page and
// redirect, either of which may be empty; by default it is a redirect.
func (f *Fake) NextConfirmationPage(html, redirectURL string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.confirmPages = append(f.confirmPages, providers.Confirmation{HTML: html, RedirectURL: redirectURL})
}

// NextCompletion queues the outcome of the next completed 3-D Secure step.
func (f *Fake) NextCompletion(state providers.PaymentState, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completions = append(f.completions, providers.ChargeResult{State: state, FailureReason: reason})
}

// SetPaymentState is what FetchPayment and FindCharge answer for ref from
// now on, as if the provider's side moved.
func (f *Fake) SetPaymentState(ref string, state providers.PaymentState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.payments[ref]; ok {
		p.state = state

		return
	}
	f.record(ref, "", state)
}

// SetCardStatus is what CardStatus answers for a card.
func (f *Fake) SetCardStatus(cardRef string, status providers.CardStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cardStatuses[cardRef] = status
}

// Captures counts the payments the provider holds as succeeded: money that
// actually moved.
func (f *Fake) Captures() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.payments {
		if p.state == providers.StateSucceeded {
			n++
		}
	}

	return n
}

// LastVerificationRef answers the reference the last StartCardVerification
// handed out.
func (f *Fake) LastVerificationRef() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return fmt.Sprintf("ver-%d", f.nextID)
}

// WebhookBody is the fake's wire shape for a notification.
type WebhookBody struct {
	EventID    string `json:"event_id"`
	Kind       string `json:"kind"`
	PaymentRef string `json:"payment_ref"`
	Status     string `json:"status"`
}

// Webhook builds a notification with a valid signature.
func Webhook(event WebhookBody) (http.Header, []byte) {
	body, _ := json.Marshal(event)
	headers := http.Header{}
	headers.Set(SignatureHeader, Sign(body))
	headers.Set("Content-Type", "application/json")

	return headers, body
}

// Sign is the signature VerifyWebhook accepts for body.
func Sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(SigningKey))
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}

// Callback is the form the fake's 3-D Secure page posts back for ref.
func Callback(ref, status string) url.Values {
	form := url.Values{}
	form.Set(CallbackPaymentID, ref)
	form.Set(CallbackStatus, status)
	form.Set(CallbackHash, Sign([]byte(ref+"|"+status)))

	return form
}

// ---------------------------------------------------------------------------
// providers.Provider
// ---------------------------------------------------------------------------

func (f *Fake) Code() string { return f.code }

func (f *Fake) StartCardVerification(_ context.Context, req providers.VerificationRequest) (*providers.Verification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Verifications = append(f.Verifications, req)
	if f.failStart != nil {
		return nil, f.failStart
	}

	f.nextID++
	ref := fmt.Sprintf("ver-%d", f.nextID)
	f.verifications[ref] = &verification{req: req}

	return &providers.Verification{Ref: ref, RedirectURL: redirectBase + ref + "?return=" + url.QueryEscape(req.ReturnURL)}, nil
}

func (f *Fake) CompleteCardVerification(_ context.Context, ref string, _ map[string]string) (*providers.SavedCard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.verifications[ref]
	if !ok {
		return nil, providers.ErrUnknownRef
	}
	if v.finished {
		return nil, &providers.DeclinedError{Reason: providers.ReasonNotCompleted}
	}
	v.finished = true

	if len(f.verifyOutcomes) > 0 {
		reason := f.verifyOutcomes[0]
		f.verifyOutcomes = f.verifyOutcomes[1:]

		return nil, &providers.DeclinedError{Reason: reason}
	}

	card := providers.SavedCard{Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030}
	if len(f.nextCards) > 0 {
		card = f.nextCards[0]
		f.nextCards = f.nextCards[1:]
	}
	card.CustomerRef = "cus-" + v.req.Payer.TeamID.String()
	card.CardRef = "card-" + ref

	return &card, nil
}

// Charge answers the queued outcome. Every call that reaches the card is a
// new payment: the fake, like Craftgate, does not dedupe by key.
func (f *Fake) Charge(ctx context.Context, req providers.ChargeRequest) (*providers.ChargeResult, error) {
	f.mu.Lock()
	h := f.hold
	f.hold = nil
	f.mu.Unlock()
	if h != nil {
		close(h.entered)
		select {
		case <-h.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	script := chargeScript{result: providers.ChargeResult{State: providers.StateSucceeded}, capture: true}
	if len(f.charges) > 0 {
		script = f.charges[0]
		f.charges = f.charges[1:]
	}
	if script.err != nil && !script.capture {
		return nil, script.err
	}

	f.Charges = append(f.Charges, req)
	result := script.result
	f.nextID++
	result.ProviderPaymentRef = fmt.Sprintf("pay-%d", f.nextID)
	result.IdempotencyKey = req.IdempotencyKey
	state := result.State
	if state == providers.StateConfirmationRequired {
		// The refused attempt is recorded as failed at the provider; the
		// confirmation is a separate payment once the cardholder starts it.
		state = providers.StateFailed
	}
	f.record(result.ProviderPaymentRef, req.IdempotencyKey, state)
	if script.err != nil {
		return nil, script.err
	}
	out := result

	return &out, nil
}

func (f *Fake) StartConfirmation(_ context.Context, req providers.ChargeRequest) (*providers.Confirmation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Confirmations = append(f.Confirmations, req)
	f.nextID++
	ref := fmt.Sprintf("3ds-%d", f.nextID)
	f.record(ref, req.IdempotencyKey+threeDSKey, providers.StateConfirmationRequired)

	out := providers.Confirmation{ProviderPaymentRef: ref, RedirectURL: confirmBase + ref + "?callback=" + url.QueryEscape(req.ReturnURL)}
	if len(f.confirmPages) > 0 {
		out.HTML, out.RedirectURL = f.confirmPages[0].HTML, f.confirmPages[0].RedirectURL
		f.confirmPages = f.confirmPages[1:]
	}

	return &out, nil
}

func (f *Fake) CompleteConfirmation(_ context.Context, callback map[string]string) (*providers.ChargeResult, error) {
	ref, status := callback[CallbackPaymentID], callback[CallbackStatus]
	if !hmac.Equal([]byte(callback[CallbackHash]), []byte(Sign([]byte(ref+"|"+status)))) {
		return nil, providers.ErrBadSignature
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.payments[ref]
	if !ok {
		return nil, providers.ErrUnknownRef
	}
	key := strings.TrimSuffix(p.key, threeDSKey)
	if status != StatusSuccess {
		p.state = providers.StateFailed

		return &providers.ChargeResult{State: providers.StateFailed, ProviderPaymentRef: ref, FailureReason: providers.ReasonNotCompleted, IdempotencyKey: key}, nil
	}

	result := providers.ChargeResult{State: providers.StateSucceeded}
	if len(f.completions) > 0 {
		result = f.completions[0]
		f.completions = f.completions[1:]
	}
	result.ProviderPaymentRef, result.IdempotencyKey = ref, key
	p.state = result.State

	return &result, nil
}

func (f *Fake) FindCharge(_ context.Context, idempotencyKey string) (*providers.ChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found *payment
	for _, ref := range f.order {
		p := f.payments[ref]
		if p.key != idempotencyKey && p.key != idempotencyKey+threeDSKey {
			continue
		}
		if found == nil || p.state == providers.StateSucceeded || found.state != providers.StateSucceeded {
			found = p
		}
	}
	if found == nil {
		return nil, providers.ErrUnknownRef
	}

	return &providers.ChargeResult{State: found.state, ProviderPaymentRef: found.ref, IdempotencyKey: idempotencyKey}, nil
}

func (f *Fake) CardStatus(_ context.Context, card providers.CardRef) (*providers.CardStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	status := f.cardStatuses[card.CardRef]

	return &status, nil
}

func (f *Fake) Refund(_ context.Context, req providers.RefundRequest) (*providers.RefundResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Refunds = append(f.Refunds, req)
	f.nextID++

	return &providers.RefundResult{ProviderRefundRef: fmt.Sprintf("ref-%d", f.nextID), State: providers.StateSucceeded}, nil
}

func (f *Fake) VerifyWebhook(_ context.Context, headers http.Header, body []byte) (*providers.WebhookEvent, error) {
	if !hmac.Equal([]byte(headers.Get(SignatureHeader)), []byte(Sign(body))) {
		return nil, providers.ErrBadSignature
	}

	var event WebhookBody
	if err := json.Unmarshal(body, &event); err != nil || str.IsBlank(event.EventID) {
		return nil, providers.ErrBadSignature
	}

	kind := providers.WebhookKind(event.Kind)
	if str.IsBlank(event.Kind) {
		kind = providers.WebhookPayment
	}

	return &providers.WebhookEvent{
		ProviderEventID:    event.EventID,
		Kind:               kind,
		ProviderPaymentRef: event.PaymentRef,
		State:              providers.PaymentState(event.Status),
	}, nil
}

func (f *Fake) FetchPayment(_ context.Context, ref string) (*providers.ChargeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.payments[ref]
	if !ok {
		return nil, providers.ErrUnknownRef
	}

	return &providers.ChargeResult{State: p.state, ProviderPaymentRef: ref, IdempotencyKey: strings.TrimSuffix(p.key, threeDSKey)}, nil
}

func (f *Fake) record(ref, key string, state providers.PaymentState) {
	f.payments[ref] = &payment{ref: ref, key: key, state: state}
	f.order = append(f.order, ref)
}
