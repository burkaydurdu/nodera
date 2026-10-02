// Package stripe is the Stripe adapter (ADR-0006), built and tested but not
// routed: payment_routes points at it once a non-Turkish entity exists. Cards
// are saved through a Checkout Session in setup mode (a SetupIntent behind a
// hosted page) and charged off-session with a PaymentIntent; a bank asking
// for authentication becomes confirmation_required.
package stripe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"
	stripeapi "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"

	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	Code            = "stripe"
	SignatureHeader = "Stripe-Signature"

	modeSetup       = "setup"
	usageOffSession = "off_session"
	methodCard      = "card"
	expandSetupPM   = "setup_intent.payment_method"
	metadataTeamID  = "team_id"
	// metadataAttempt records the attempt's idempotency key on the
	// PaymentIntent, so the lookup by key works through search.
	metadataAttempt  = "idempotency_key"
	centsPerUnit     = 100
	noNetworkRetries = 0
)

type Config struct {
	SecretKey     string
	WebhookSecret string
	// BaseURL overrides the API host; empty means Stripe's.
	BaseURL string
}

type Stripe struct {
	client        *stripeapi.Client
	webhookSecret string
	log           logger.ILog
}

var ErrMissingCredentials = errors.New("stripe secret key and webhook secret are required")

func New(cfg Config, log logger.ILog) (*Stripe, error) {
	if str.IsBlank(cfg.SecretKey) || str.IsBlank(cfg.WebhookSecret) {
		return nil, ErrMissingCredentials
	}

	var opts []stripeapi.ClientOption
	if str.IsNotEmpty(cfg.BaseURL) {
		opts = append(opts, stripeapi.WithBackends(stripeapi.NewBackendsWithConfig(&stripeapi.BackendConfig{
			URL:               stripeapi.String(cfg.BaseURL),
			MaxNetworkRetries: stripeapi.Int64(noNetworkRetries),
		})))
	}

	return &Stripe{client: stripeapi.NewClient(cfg.SecretKey, opts...), webhookSecret: cfg.WebhookSecret, log: log}, nil
}

func (s *Stripe) Code() string { return Code }

// StartCardVerification creates the Customer the card will belong to and a
// setup-mode Checkout Session that saves the card for off-session use.
func (s *Stripe) StartCardVerification(ctx context.Context, req providers.VerificationRequest) (*providers.Verification, error) {
	customerParams := &stripeapi.CustomerCreateParams{
		Email:    stripeapi.String(req.Payer.Email),
		Name:     stripeapi.String(req.Payer.LegalName),
		Metadata: map[string]string{metadataTeamID: req.Payer.TeamID.String()},
	}
	customerParams.SetIdempotencyKey("verification:" + req.ReferenceID + ":customer")
	customer, err := s.client.V1Customers.Create(ctx, customerParams)
	if err != nil {
		return nil, s.unavailable("create customer", err)
	}

	sessionParams := &stripeapi.CheckoutSessionCreateParams{
		Mode:               stripeapi.String(modeSetup),
		Customer:           stripeapi.String(customer.ID),
		Currency:           stripeapi.String(strings.ToLower(req.Currency)),
		PaymentMethodTypes: []*string{stripeapi.String(methodCard)},
		SuccessURL:         stripeapi.String(req.ReturnURL),
		CancelURL:          stripeapi.String(req.ReturnURL),
		ClientReferenceID:  stripeapi.String(req.ReferenceID),
		SetupIntentData: &stripeapi.CheckoutSessionCreateSetupIntentDataParams{
			Metadata: map[string]string{metadataTeamID: req.Payer.TeamID.String()},
		},
	}
	sessionParams.SetIdempotencyKey("verification:" + req.ReferenceID + ":session")
	session, err := s.client.V1CheckoutSessions.Create(ctx, sessionParams)
	if err != nil {
		return nil, s.unavailable("create checkout session", err)
	}

	return &providers.Verification{Ref: session.ID, RedirectURL: session.URL}, nil
}

// CompleteCardVerification reads the session's SetupIntent: succeeded means
// the card is saved on the Customer for off-session charges.
func (s *Stripe) CompleteCardVerification(ctx context.Context, ref string, _ map[string]string) (*providers.SavedCard, error) {
	params := &stripeapi.CheckoutSessionRetrieveParams{}
	params.AddExpand(expandSetupPM)
	session, err := s.client.V1CheckoutSessions.Retrieve(ctx, ref, params)
	if err != nil {
		if isMissing(err) {
			return nil, providers.ErrUnknownRef
		}

		return nil, s.unavailable("retrieve checkout session", err)
	}

	intent := session.SetupIntent
	if intent == nil || intent.Status != stripeapi.SetupIntentStatusSucceeded {
		reason := providers.ReasonNotCompleted
		if intent != nil && intent.LastSetupError != nil {
			reason = declineReason(intent.LastSetupError)
		}

		return nil, &providers.DeclinedError{Reason: reason}
	}
	if session.Customer == nil || intent.PaymentMethod == nil || intent.PaymentMethod.Card == nil {
		return nil, s.unavailable("retrieve checkout session", errors.New("succeeded setup without a card"))
	}

	card := intent.PaymentMethod.Card

	return &providers.SavedCard{
		CustomerRef: session.Customer.ID,
		CardRef:     intent.PaymentMethod.ID,
		Brand:       string(card.Brand),
		Last4:       card.Last4,
		ExpMonth:    int(card.ExpMonth),
		ExpYear:     int(card.ExpYear),
	}, nil
}

// Charge confirms an off-session PaymentIntent at once; Stripe answers an
// authentication demand as a card error carrying the PaymentIntent.
func (s *Stripe) Charge(ctx context.Context, req providers.ChargeRequest) (*providers.ChargeResult, error) {
	params := &stripeapi.PaymentIntentCreateParams{
		Amount:             stripeapi.Int64(req.Amount.Mul(decimalHundred).Round(0).IntPart()),
		Currency:           stripeapi.String(strings.ToLower(req.Currency)),
		Customer:           stripeapi.String(req.Card.CustomerRef),
		PaymentMethod:      stripeapi.String(req.Card.CardRef),
		PaymentMethodTypes: []*string{stripeapi.String(methodCard)},
		OffSession:         stripeapi.Bool(true),
		Confirm:            stripeapi.Bool(true),
		Description:        stripeapi.String(req.Description),
		Metadata:           map[string]string{metadataAttempt: req.IdempotencyKey},
	}
	params.SetIdempotencyKey(req.IdempotencyKey)

	intent, err := s.client.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		var stripeErr *stripeapi.Error
		if !errors.As(err, &stripeErr) || stripeErr.Type != stripeapi.ErrorTypeCard {
			return nil, s.unavailable("create payment intent", err)
		}
		ref := ""
		if stripeErr.PaymentIntent != nil {
			ref = stripeErr.PaymentIntent.ID
		}
		if stripeErr.Code == stripeapi.ErrorCodeAuthenticationRequired {
			return &providers.ChargeResult{State: providers.StateConfirmationRequired, ProviderPaymentRef: ref}, nil
		}

		return &providers.ChargeResult{State: providers.StateFailed, ProviderPaymentRef: ref, FailureReason: declineReason(stripeErr)}, nil
	}

	return resultOf(intent), nil
}

// StartConfirmation confirms the refused PaymentIntent on-session with a
// return URL, which makes Stripe answer the page the cardholder
// authenticates on.
func (s *Stripe) StartConfirmation(ctx context.Context, req providers.ChargeRequest) (*providers.Confirmation, error) {
	if str.IsBlank(req.ProviderPaymentRef) {
		return nil, providers.ErrUnknownRef
	}
	intent, err := s.client.V1PaymentIntents.Confirm(ctx, req.ProviderPaymentRef, &stripeapi.PaymentIntentConfirmParams{
		ReturnURL: stripeapi.String(req.ReturnURL),
	})
	if err != nil {
		if isMissing(err) {
			return nil, providers.ErrUnknownRef
		}
		var stripeErr *stripeapi.Error
		if errors.As(err, &stripeErr) && stripeErr.Type == stripeapi.ErrorTypeCard {
			return nil, &providers.DeclinedError{Reason: declineReason(stripeErr)}
		}

		return nil, s.unavailable("confirm payment intent", err)
	}
	if intent.NextAction == nil || intent.NextAction.RedirectToURL == nil || str.IsBlank(intent.NextAction.RedirectToURL.URL) {
		return nil, s.unavailable("confirm payment intent", errors.New("no redirect to authenticate on"))
	}

	return &providers.Confirmation{ProviderPaymentRef: intent.ID, RedirectURL: intent.NextAction.RedirectToURL.URL}, nil
}

// CompleteConfirmation reads the PaymentIntent the return URL names. Stripe
// signs no redirect, so the intent's state fetched from the API is the only
// thing trusted; a callback naming no intent is not authentic.
func (s *Stripe) CompleteConfirmation(ctx context.Context, callback map[string]string) (*providers.ChargeResult, error) {
	ref := callback["payment_intent"]
	if str.IsBlank(ref) {
		return nil, providers.ErrBadSignature
	}

	return s.FetchPayment(ctx, ref)
}

// FindCharge searches PaymentIntents by the attempt's key in their metadata.
func (s *Stripe) FindCharge(ctx context.Context, idempotencyKey string) (*providers.ChargeResult, error) {
	params := &stripeapi.PaymentIntentSearchParams{}
	params.Query = fmt.Sprintf("metadata['%s']:'%s'", metadataAttempt, idempotencyKey)
	params.Single = true

	var best *providers.ChargeResult
	for intent, err := range s.client.V1PaymentIntents.Search(ctx, params) {
		if err != nil {
			return nil, s.unavailable("search payment intents", err)
		}
		found := resultOf(intent)
		if best == nil || found.State == providers.StateSucceeded {
			best = found
		}
	}
	if best == nil {
		return nil, providers.ErrUnknownRef
	}

	return best, nil
}

// CardStatus reads the PaymentMethod, which carries the card's expiry.
func (s *Stripe) CardStatus(ctx context.Context, card providers.CardRef) (*providers.CardStatus, error) {
	method, err := s.client.V1PaymentMethods.Retrieve(ctx, card.CardRef, nil)
	if err != nil {
		if isMissing(err) {
			return nil, providers.ErrUnknownRef
		}

		return nil, s.unavailable("retrieve payment method", err)
	}
	out := &providers.CardStatus{}
	if method.Card != nil {
		out.ExpMonth, out.ExpYear = int(method.Card.ExpMonth), int(method.Card.ExpYear)
	}

	return out, nil
}

func (s *Stripe) Refund(ctx context.Context, req providers.RefundRequest) (*providers.RefundResult, error) {
	params := &stripeapi.RefundCreateParams{
		PaymentIntent: stripeapi.String(req.ProviderPaymentRef),
		Amount:        stripeapi.Int64(req.Amount.Mul(decimalHundred).Round(0).IntPart()),
	}
	params.SetIdempotencyKey(req.IdempotencyKey)
	refund, err := s.client.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, s.unavailable("create refund", err)
	}

	state := providers.StatePending
	switch refund.Status {
	case stripeapi.RefundStatusSucceeded:
		state = providers.StateSucceeded
	case stripeapi.RefundStatusFailed, stripeapi.RefundStatusCanceled:
		state = providers.StateFailed
	}

	return &providers.RefundResult{ProviderRefundRef: refund.ID, State: state}, nil
}

// VerifyWebhook checks the Stripe-Signature header against the endpoint
// secret. An event from another API version is still accepted: only its id,
// type and object id are read.
func (s *Stripe) VerifyWebhook(_ context.Context, headers http.Header, body []byte) (*providers.WebhookEvent, error) {
	event, err := webhook.ConstructEventWithOptions(body, headers.Get(SignatureHeader), s.webhookSecret,
		webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		return nil, providers.ErrBadSignature
	}

	ref := ""
	if event.Data != nil {
		ref, _ = event.Data.Object["id"].(string)
	}
	kind, state := classify(string(event.Type))

	return &providers.WebhookEvent{ProviderEventID: event.ID, Kind: kind, ProviderPaymentRef: ref, State: state}, nil
}

func (s *Stripe) FetchPayment(ctx context.Context, ref string) (*providers.ChargeResult, error) {
	intent, err := s.client.V1PaymentIntents.Retrieve(ctx, ref, nil)
	if err != nil {
		if isMissing(err) {
			return nil, providers.ErrUnknownRef
		}

		return nil, s.unavailable("retrieve payment intent", err)
	}

	return resultOf(intent), nil
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

func resultOf(intent *stripeapi.PaymentIntent) *providers.ChargeResult {
	out := &providers.ChargeResult{ProviderPaymentRef: intent.ID, State: providers.StatePending, IdempotencyKey: intent.Metadata[metadataAttempt]}

	switch intent.Status {
	case stripeapi.PaymentIntentStatusSucceeded:
		out.State = providers.StateSucceeded
	case stripeapi.PaymentIntentStatusRequiresAction, stripeapi.PaymentIntentStatusRequiresConfirmation:
		out.State = providers.StateConfirmationRequired
	case stripeapi.PaymentIntentStatusRequiresPaymentMethod, stripeapi.PaymentIntentStatusCanceled:
		out.State = providers.StateFailed
		out.FailureReason = providers.ReasonDeclined
		if intent.LastPaymentError != nil {
			out.FailureReason = declineReason(intent.LastPaymentError)
		}
	}

	return out
}

func classify(eventType string) (providers.WebhookKind, providers.PaymentState) {
	switch eventType {
	case "payment_intent.succeeded":
		return providers.WebhookPayment, providers.StateSucceeded
	case "payment_intent.payment_failed", "payment_intent.canceled":
		return providers.WebhookPayment, providers.StateFailed
	case "payment_intent.requires_action":
		return providers.WebhookPayment, providers.StateConfirmationRequired
	case "payment_intent.processing":
		return providers.WebhookPayment, providers.StatePending
	}
	if strings.HasPrefix(eventType, "refund.") || strings.HasPrefix(eventType, "charge.refund") {
		return providers.WebhookRefund, providers.StatePending
	}

	return providers.WebhookOther, providers.StatePending
}

func declineReason(err *stripeapi.Error) string {
	switch {
	case err.DeclineCode == stripeapi.DeclineCodeInsufficientFunds:
		return providers.ReasonInsufficientFunds
	case err.DeclineCode == stripeapi.DeclineCodeFraudulent:
		return providers.ReasonFraudSuspected
	case err.Code == stripeapi.ErrorCodeExpiredCard:
		return providers.ReasonExpiredCard
	case err.Code == stripeapi.ErrorCodeIncorrectNumber || err.Code == stripeapi.ErrorCodeInvalidNumber:
		return providers.ReasonInvalidCard
	default:
		return providers.ReasonDeclined
	}
}

func isMissing(err error) bool {
	var stripeErr *stripeapi.Error

	return errors.As(err, &stripeErr) && (stripeErr.Code == stripeapi.ErrorCodeResourceMissing || stripeErr.HTTPStatusCode == http.StatusNotFound)
}

func (s *Stripe) unavailable(step string, err error) error {
	s.log.ErrorWithFields(err, logger.NewFields().Add("step", step))

	return fmt.Errorf("payment provider %s: %w", step, err)
}

// decimalHundred turns a USD amount into the integer cents Stripe takes.
var decimalHundred = decimal.NewFromInt(centsPerUnit)
