// Package craftgate is the Craftgate adapter (ADR-0006) over the official
// client. Everything Craftgate-specific stays here: the hosted checkout form
// used as card verification, cardUserKey/cardToken storage, the recurring
// payment group, the non-3DS charge with 3-D Secure opened later as the
// fallback, the callback and webhook checks, and the lookup by conversation
// id that stands in for the idempotency Craftgate does not offer. Raw error
// texts are logged, never answered.
package craftgate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/craftgate/craftgate-go-client/adapter"
	"github.com/shopspring/decimal"

	"github.com/piggystats/nodera/contexts/billing/providers"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/utils/str"
)

const (
	Code = "craftgate"
	// SignatureHeader carries the webhook's base64 HMAC-SHA256.
	SignatureHeader = "X-CG-SIGNATURE-V1"
	// DefaultBaseURL is the production API; the sandbox is
	// https://sandbox-api.craftgate.io.
	DefaultBaseURL = "https://api.craftgate.io"

	verificationItem = "Card verification"
	singleInstalment = 1
	// threeDSSuffix marks the 3-D Secure payment of an attempt; Craftgate
	// keeps conversation ids unique per payment, not per attempt.
	threeDSSuffix = ":3ds"
	searchSize    = 20
	callbackOK    = "SUCCESS"
)

// verificationAmount is the pre-authorisation the hosted form holds to prove
// the card; it is released right after the card is saved.
var verificationAmount = decimal.NewFromInt(1)

type Config struct {
	APIKey     string
	SecretKey  string
	BaseURL    string
	WebhookKey string
	// ThreeDSCallbackKey is the merchant key Craftgate hashes every 3-D
	// Secure callback with.
	ThreeDSCallbackKey string
}

// paymentAPI is the slice of the client's payment service the adapter
// drives; *adapter.Payment satisfies it.
type paymentAPI interface {
	InitCheckoutPayment(ctx context.Context, request adapter.InitCheckoutPaymentRequest) (*adapter.InitCheckoutPaymentResponse, error)
	RetrieveCheckoutPayment(ctx context.Context, token string) (*adapter.PaymentResponse, error)
	CreatePayment(ctx context.Context, request adapter.CreatePaymentRequest) (*adapter.PaymentResponse, error)
	Init3DSPayment(ctx context.Context, request adapter.Init3DSPaymentRequest) (*adapter.Init3DSPaymentResponse, error)
	Complete3DSPayment(ctx context.Context, request adapter.Complete3DSPaymentRequest) (*adapter.PaymentResponse, error)
	RetrievePayment(ctx context.Context, paymentID int64) (*adapter.PaymentResponse, error)
	RefundPayment(ctx context.Context, request adapter.RefundPaymentRequest) (*adapter.PaymentRefundResponse, error)
	SearchStoredCards(ctx context.Context, request adapter.SearchStoredCardsRequest) (*adapter.DataResponse[adapter.StoredCardResponse], error)
}

// reportingAPI is the slice of the payment-reporting service the lookup by
// conversation id needs.
type reportingAPI interface {
	SearchPayments(ctx context.Context, request adapter.SearchPaymentsRequest) (*adapter.DataResponse[adapter.ReportingPaymentResponse], error)
}

type Craftgate struct {
	payments    paymentAPI
	reporting   reportingAPI
	webhookKey  string
	callbackKey string
	log         logger.ILog
}

var ErrMissingCredentials = errors.New("craftgate api key, secret key, webhook key and 3-D Secure callback key are required")

func New(cfg Config, log logger.ILog) (*Craftgate, error) {
	if str.IsBlank(cfg.APIKey) || str.IsBlank(cfg.SecretKey) || str.IsBlank(cfg.WebhookKey) || str.IsBlank(cfg.ThreeDSCallbackKey) {
		return nil, ErrMissingCredentials
	}
	baseURL := cfg.BaseURL
	if str.IsBlank(baseURL) {
		baseURL = DefaultBaseURL
	}

	// The client would otherwise take http.DefaultClient and set a two-hour
	// timeout on it, for the whole process.
	client, err := adapter.New(cfg.APIKey, cfg.SecretKey, baseURL, adapter.WithCustomHTTPClient(&http.Client{Timeout: providers.CallTimeout}))
	if err != nil {
		return nil, err
	}

	return &Craftgate{
		payments: client.Payment, reporting: client.PaymentReporting,
		webhookKey: cfg.WebhookKey, callbackKey: cfg.ThreeDSCallbackKey, log: log,
	}, nil
}

func (c *Craftgate) Code() string { return Code }

// StartCardVerification opens Craftgate's hosted checkout as a verification:
// a small pre-authorisation with 3-D Secure forced and the card stored after
// success, so the later merchant-initiated charges need no cardholder.
func (c *Craftgate) StartCardVerification(ctx context.Context, req providers.VerificationRequest) (*providers.Verification, error) {
	price := amount(verificationAmount)
	res, err := c.payments.InitCheckoutPayment(ctx, adapter.InitCheckoutPaymentRequest{
		Price:                       price,
		PaidPrice:                   price,
		Currency:                    adapter.Currency(req.Currency),
		PaymentGroup:                adapter.PaymentGroup_LISTING_OR_SUBSCRIPTION,
		PaymentPhase:                adapter.PaymentPhase_PRE_AUTH,
		ConversationId:              req.ReferenceID,
		ExternalId:                  req.Payer.TeamID.String(),
		CallbackUrl:                 req.ReturnURL,
		ForceThreeDS:                true,
		AlwaysStoreCardAfterPayment: true,
		EnabledPaymentMethods:       []adapter.PaymentMethod{adapter.PaymentMethod_CARD},
		EnabledInstallments:         []int{singleInstalment},
		Items:                       []adapter.PaymentItem{{Name: verificationItem, Price: price, ExternalId: req.ReferenceID}},
	})
	if err != nil {
		return nil, c.unavailable("init checkout", err)
	}
	if res == nil || res.Token == nil || res.PageUrl == nil {
		return nil, c.unavailable("init checkout", errors.New("response without token or page url"))
	}

	return &providers.Verification{Ref: *res.Token, RedirectURL: *res.PageUrl}, nil
}

// CompleteCardVerification reads the checkout's payment: a successful one
// carries the stored card's keys. The pre-authorisation is then released;
// a failed release is logged, it lapses on its own.
func (c *Craftgate) CompleteCardVerification(ctx context.Context, ref string, _ map[string]string) (*providers.SavedCard, error) {
	res, err := c.payments.RetrieveCheckoutPayment(ctx, ref)
	if err != nil {
		if cgErr, ok := asCraftgateError(err); ok {
			c.log.WarnWithFields("craftgate checkout retrieve refused", logger.NewFields().Add("group", deref(cgErr.ErrorGroup)).Add("code", deref(cgErr.ErrorCode)))

			return nil, providers.ErrUnknownRef
		}

		return nil, c.unavailable("retrieve checkout", err)
	}
	if res == nil || res.PaymentStatus == nil || *res.PaymentStatus != adapter.PaymentStatus_SUCCESS {
		reason := providers.ReasonNotCompleted
		if res != nil && res.PaymentError != nil {
			reason = declineReason(res.PaymentError.ErrorGroup)
		}

		return nil, &providers.DeclinedError{Reason: reason}
	}
	if res.CardUserKey == nil || res.CardToken == nil {
		return nil, c.unavailable("retrieve checkout", errors.New("successful checkout without a stored card"))
	}

	if res.Id != nil {
		if _, err := c.payments.RefundPayment(ctx, adapter.RefundPaymentRequest{PaymentId: *res.Id, ConversationId: ref}); err != nil {
			c.log.ErrorWithFields(c.describe(err), logger.NewFields().Add("payment", *res.Id).Add("step", "release verification"))
		}
	}

	card := &providers.SavedCard{
		CustomerRef: *res.CardUserKey,
		CardRef:     *res.CardToken,
		Brand:       brand(res.CardAssociation),
		Last4:       deref(res.LastFourDigits),
	}
	// The payment response does not carry the expiry; a gateway that returns
	// it in additionalData is read, otherwise it stays unknown (0).
	card.ExpMonth, card.ExpYear = expiryFrom(res.AdditionalData)

	return card, nil
}

// Charge tries the merchant-initiated charge without 3-D Secure. A hard
// decline is final; any other bank refusal answers confirmation required
// without opening 3-D Secure, which waits for the cardholder to open the
// link. An error Craftgate does not classify as a bank decline is the
// provider being unavailable: the attempt stays open and is looked up later.
func (c *Craftgate) Charge(ctx context.Context, req providers.ChargeRequest) (*providers.ChargeResult, error) {
	price := amount(req.Amount)
	res, err := c.payments.CreatePayment(ctx, adapter.CreatePaymentRequest{
		Price:          price,
		PaidPrice:      price,
		Currency:       adapter.Currency(req.Currency),
		Installment:    singleInstalment,
		PaymentGroup:   adapter.PaymentGroup_LISTING_OR_SUBSCRIPTION,
		PaymentPhase:   adapter.PaymentPhase_AUTH,
		ConversationId: req.IdempotencyKey,
		ExternalId:     req.IdempotencyKey,
		Card:           &adapter.Card{CardUserKey: req.Card.CustomerRef, CardToken: req.Card.CardRef},
		Items:          []adapter.PaymentItem{{Name: req.Description, Price: price, ExternalId: req.IdempotencyKey}},
	})

	var (
		group *string
		ref   string
	)
	switch {
	case err == nil && res != nil && res.PaymentStatus != nil && *res.PaymentStatus == adapter.PaymentStatus_SUCCESS:
		return &providers.ChargeResult{State: providers.StateSucceeded, ProviderPaymentRef: paymentRef(res.Id), IdempotencyKey: req.IdempotencyKey}, nil
	case err == nil && res != nil && res.PaymentError != nil:
		group, ref = res.PaymentError.ErrorGroup, paymentRef(res.Id)
	case err == nil && res != nil && res.PaymentStatus != nil && *res.PaymentStatus == adapter.PaymentStatus_WAITING:
		return &providers.ChargeResult{State: providers.StatePending, ProviderPaymentRef: paymentRef(res.Id), IdempotencyKey: req.IdempotencyKey}, nil
	case err != nil:
		cgErr, ok := asCraftgateError(err)
		if !ok {
			return nil, c.unavailable("create payment", err)
		}
		group = cgErr.ErrorGroup
	default:
		return nil, c.unavailable("create payment", errors.New("response without a payment status"))
	}

	if !isBankDecline(group) {
		return nil, c.unavailable("create payment", fmt.Errorf("craftgate error group %s", deref(group)))
	}
	if isHardDecline(group) {
		return &providers.ChargeResult{State: providers.StateFailed, ProviderPaymentRef: ref, FailureReason: declineReason(group), IdempotencyKey: req.IdempotencyKey}, nil
	}

	return &providers.ChargeResult{State: providers.StateConfirmationRequired, ProviderPaymentRef: ref, IdempotencyKey: req.IdempotencyKey}, nil
}

// StartConfirmation opens the 3-D Secure payment of an attempt when the
// cardholder is there to take it. Its conversation id is the attempt's key
// with a suffix, so the lookup by key finds it too.
func (c *Craftgate) StartConfirmation(ctx context.Context, req providers.ChargeRequest) (*providers.Confirmation, error) {
	price := amount(req.Amount)
	res, err := c.payments.Init3DSPayment(ctx, adapter.Init3DSPaymentRequest{
		Price:          price,
		PaidPrice:      price,
		Currency:       adapter.Currency(req.Currency),
		Installment:    singleInstalment,
		PaymentGroup:   adapter.PaymentGroup_LISTING_OR_SUBSCRIPTION,
		PaymentPhase:   adapter.PaymentPhase_AUTH,
		ConversationId: req.IdempotencyKey + threeDSSuffix,
		ExternalId:     req.IdempotencyKey,
		CallbackUrl:    req.ReturnURL,
		Card:           &adapter.Card{CardUserKey: req.Card.CustomerRef, CardToken: req.Card.CardRef},
		Items:          []adapter.PaymentItem{{Name: req.Description, Price: price, ExternalId: req.IdempotencyKey}},
	})
	if err != nil {
		if cgErr, ok := asCraftgateError(err); ok && isBankDecline(cgErr.ErrorGroup) {
			return nil, &providers.DeclinedError{Reason: declineReason(cgErr.ErrorGroup)}
		}

		return nil, c.unavailable("init 3ds", err)
	}

	out := &providers.Confirmation{ProviderPaymentRef: paymentRef(res.PaymentId), RedirectURL: deref(res.RedirectUrl)}
	if res.HtmlContent != nil {
		decoded, err := base64.StdEncoding.DecodeString(*res.HtmlContent)
		if err != nil {
			return nil, c.unavailable("init 3ds", err)
		}
		out.HTML = string(decoded)
	}
	if str.IsBlank(out.HTML) && str.IsBlank(out.RedirectURL) {
		return nil, c.unavailable("init 3ds", errors.New("response without html content or redirect url"))
	}

	return out, nil
}

// CompleteConfirmation checks the callback's hash with the merchant's 3-D
// Secure callback key before anything else, then completes the payment the
// callback names. A failed cardholder check never reaches the bank.
func (c *Craftgate) CompleteConfirmation(ctx context.Context, callback map[string]string) (*providers.ChargeResult, error) {
	if !c.callbackVerified(callback) {
		return nil, providers.ErrBadSignature
	}
	ref := callback["paymentId"]
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return nil, providers.ErrBadSignature
	}
	key := strings.TrimSuffix(callback["conversationId"], threeDSSuffix)
	if callback["status"] != callbackOK {
		return &providers.ChargeResult{State: providers.StateFailed, ProviderPaymentRef: ref, FailureReason: providers.ReasonNotCompleted, IdempotencyKey: key}, nil
	}

	res, err := c.payments.Complete3DSPayment(ctx, adapter.Complete3DSPaymentRequest{PaymentId: id})
	if err != nil {
		if cgErr, ok := asCraftgateError(err); ok && isBankDecline(cgErr.ErrorGroup) {
			return &providers.ChargeResult{State: providers.StateFailed, ProviderPaymentRef: ref, FailureReason: declineReason(cgErr.ErrorGroup), IdempotencyKey: key}, nil
		}

		return nil, c.unavailable("complete 3ds", err)
	}
	out := resultOf(res, ref)
	if str.IsBlank(out.IdempotencyKey) {
		out.IdempotencyKey = key
	}

	return out, nil
}

// callbackVerified recomputes Craftgate's documented callback hash and
// compares it in constant time.
func (c *Craftgate) callbackVerified(callback map[string]string) bool {
	joined := strings.Join([]string{
		c.callbackKey, callback["status"], callback["completeStatus"], callback["paymentId"],
		callback["conversationData"], callback["conversationId"], callback["callbackStatus"],
	}, "###")
	sum := sha256.Sum256([]byte(joined))
	expected := hex.EncodeToString(sum[:])

	return subtle.ConstantTimeCompare([]byte(expected), []byte(callback["hash"])) == 1
}

// FindCharge searches the attempt's payments by conversation id, its own
// and its 3-D Secure one, and answers the one that matters most: a captured
// payment over one still open over a refusal.
func (c *Craftgate) FindCharge(ctx context.Context, idempotencyKey string) (*providers.ChargeResult, error) {
	var best *providers.ChargeResult
	for _, conversation := range []string{idempotencyKey, idempotencyKey + threeDSSuffix} {
		res, err := c.reporting.SearchPayments(ctx, adapter.SearchPaymentsRequest{ConversationId: conversation, Size: searchSize})
		if err != nil {
			return nil, c.unavailable("search payments", err)
		}
		if res == nil {
			continue
		}
		for _, item := range res.Items {
			found := reportedResult(&item, idempotencyKey)
			if best == nil || rank(found.State) > rank(best.State) {
				best = found
			}
		}
	}
	if best == nil {
		return nil, providers.ErrUnknownRef
	}

	return best, nil
}

// CardStatus asks Craftgate's stored-card search: it reports whether the
// card expires next month but not the month itself.
func (c *Craftgate) CardStatus(ctx context.Context, card providers.CardRef) (*providers.CardStatus, error) {
	res, err := c.payments.SearchStoredCards(ctx, adapter.SearchStoredCardsRequest{CardUserKey: card.CustomerRef, CardToken: card.CardRef})
	if err != nil {
		return nil, c.unavailable("search stored cards", err)
	}
	out := &providers.CardStatus{}
	if res == nil || len(res.Items) == 0 || res.Items[0].CardExpiryStatus == nil {
		return out, nil
	}
	switch *res.Items[0].CardExpiryStatus {
	case adapter.CardExpiryStatus_WILL_EXPIRE_NEXT_MONTH:
		out.ExpiringSoon = true
	case adapter.CardExpiryStatus_EXPIRED:
		out.Expired = true
	}

	return out, nil
}

func (c *Craftgate) Refund(ctx context.Context, req providers.RefundRequest) (*providers.RefundResult, error) {
	id, err := strconv.ParseInt(req.ProviderPaymentRef, 10, 64)
	if err != nil {
		return nil, providers.ErrUnknownRef
	}

	res, err := c.payments.RefundPayment(ctx, adapter.RefundPaymentRequest{
		PaymentId:             id,
		ConversationId:        req.IdempotencyKey,
		RefundDestinationType: adapter.RefundDestinationType_PROVIDER,
	})
	if err != nil {
		return nil, c.unavailable("refund", err)
	}

	out := &providers.RefundResult{ProviderRefundRef: paymentRef(res.Id), State: providers.StatePending}
	if res.Status != nil && strings.EqualFold(string(*res.Status), "SUCCESS") {
		out.State = providers.StateSucceeded
	}

	return out, nil
}

// webhookBody is Craftgate's notification as it arrives.
type webhookBody struct {
	EventType      string `json:"eventType"`
	EventTimestamp int64  `json:"eventTimestamp"`
	Status         string `json:"status"`
	PayloadID      string `json:"payloadId"`
}

// VerifyWebhook recomputes the merchant-key HMAC Craftgate signs every
// webhook with and compares it in constant time. Craftgate sends no event
// id, so the signed fields themselves are the dedupe key.
func (c *Craftgate) VerifyWebhook(_ context.Context, headers http.Header, body []byte) (*providers.WebhookEvent, error) {
	var hook webhookBody
	if err := json.Unmarshal(body, &hook); err != nil {
		return nil, providers.ErrBadSignature
	}
	signature := headers.Get(SignatureHeader)
	if str.IsBlank(signature) || str.IsBlank(hook.EventType) || str.IsBlank(hook.PayloadID) {
		return nil, providers.ErrBadSignature
	}

	mac := hmac.New(sha256.New, []byte(c.webhookKey))
	_, _ = fmt.Fprintf(mac, "%s%d%s%s", hook.EventType, hook.EventTimestamp, hook.Status, hook.PayloadID)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return nil, providers.ErrBadSignature
	}

	return &providers.WebhookEvent{
		ProviderEventID:    fmt.Sprintf("%s:%s:%d:%s", hook.EventType, hook.PayloadID, hook.EventTimestamp, hook.Status),
		Kind:               webhookKind(adapter.WebhookEventType(hook.EventType)),
		ProviderPaymentRef: hook.PayloadID,
		State:              webhookState(adapter.WebhookStatus(hook.Status)),
	}, nil
}

func (c *Craftgate) FetchPayment(ctx context.Context, ref string) (*providers.ChargeResult, error) {
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return nil, providers.ErrUnknownRef
	}

	res, err := c.payments.RetrievePayment(ctx, id)
	if err != nil {
		if cgErr, ok := asCraftgateError(err); ok && strings.EqualFold(deref(cgErr.ErrorGroup), "NOT_FOUND") {
			return nil, providers.ErrUnknownRef
		}

		return nil, c.unavailable("retrieve payment", err)
	}

	return resultOf(res, ref), nil
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

func resultOf(res *adapter.PaymentResponse, ref string) *providers.ChargeResult {
	out := &providers.ChargeResult{State: providers.StatePending, ProviderPaymentRef: ref}
	if res == nil {
		return out
	}
	out.IdempotencyKey = strings.TrimSuffix(deref(res.ConversationId), threeDSSuffix)
	if res.PaymentStatus == nil {
		return out
	}

	switch *res.PaymentStatus {
	case adapter.PaymentStatus_SUCCESS:
		out.State = providers.StateSucceeded
	case adapter.PaymentStatus_FAILURE:
		out.State = providers.StateFailed
		out.FailureReason = providers.ReasonDeclined
		if res.PaymentError != nil {
			out.FailureReason = declineReason(res.PaymentError.ErrorGroup)
		}
	case adapter.PaymentStatus_INIT_THREEDS, adapter.PaymentStatus_CALLBACK_THREEDS:
		out.State = providers.StateConfirmationRequired
	case adapter.PaymentStatus_WAITING:
		out.State = providers.StatePending
	}

	return out
}

func reportedResult(item *adapter.ReportingPaymentResponse, key string) *providers.ChargeResult {
	out := &providers.ChargeResult{State: providers.StatePending, ProviderPaymentRef: paymentRef(item.Id), IdempotencyKey: key}
	if item.PaymentStatus == nil {
		return out
	}
	switch *item.PaymentStatus {
	case adapter.PaymentStatus_SUCCESS:
		out.State = providers.StateSucceeded
	case adapter.PaymentStatus_FAILURE:
		out.State, out.FailureReason = providers.StateFailed, providers.ReasonDeclined
		if item.PaymentError != nil {
			out.FailureReason = declineReason(item.PaymentError.ErrorGroup)
		}
	case adapter.PaymentStatus_INIT_THREEDS, adapter.PaymentStatus_CALLBACK_THREEDS:
		out.State = providers.StateConfirmationRequired
	}

	return out
}

// rank orders what a lookup may find: money that moved beats an open
// payment, which beats a refusal.
func rank(state providers.PaymentState) int {
	switch state {
	case providers.StateSucceeded:
		return 3
	case providers.StatePending, providers.StateConfirmationRequired:
		return 2
	default:
		return 1
	}
}

// softDeclines are bank refusals the cardholder can turn around through
// 3-D Secure; with hardDeclines they are the only error groups that count
// as a decline. Any other group (validation, system, timeout) means
// Craftgate or the bank did not decide, so the attempt stays open.
var softDeclines = map[string]bool{
	"DO_NOT_HONOUR":                   true,
	"INVALID_TRANSACTION":             true,
	"NOT_PERMITTED_TO_CARDHOLDER":     true,
	"NOT_PERMITTED_TO_TERMINAL":       true,
	"EXCEEDS_WITHDRAWAL_AMOUNT_LIMIT": true,
	"INVALID_CVC2":                    true,
	"THREEDS_REQUIRED":                true,
}

// hardDeclines are refusals 3-D Secure cannot turn around: the card or the
// funds are the problem, not the missing cardholder.
var hardDeclines = map[string]string{
	"NOT_SUFFICIENT_FUNDS": providers.ReasonInsufficientFunds,
	"EXPIRED_CARD":         providers.ReasonExpiredCard,
	"INVALID_CARD":         providers.ReasonInvalidCard,
	"INVALID_CARD_NUMBER":  providers.ReasonInvalidCard,
	"LOST_CARD":            providers.ReasonInvalidCard,
	"STOLEN_CARD":          providers.ReasonInvalidCard,
	"RESTRICTED_CARD":      providers.ReasonInvalidCard,
	"PICKUP_CARD":          providers.ReasonInvalidCard,
	"FRAUD_SUSPECT":        providers.ReasonFraudSuspected,
	"BLOCKED_CARD":         providers.ReasonInvalidCard,
	"CLOSED_CARD":          providers.ReasonInvalidCard,
}

func isBankDecline(group *string) bool {
	return isHardDecline(group) || softDeclines[strings.ToUpper(deref(group))]
}

func isHardDecline(group *string) bool {
	_, ok := hardDeclines[strings.ToUpper(deref(group))]

	return ok
}

func declineReason(group *string) string {
	if reason, ok := hardDeclines[strings.ToUpper(deref(group))]; ok {
		return reason
	}

	return providers.ReasonDeclined
}

func webhookKind(t adapter.WebhookEventType) providers.WebhookKind {
	switch t {
	case adapter.WebhookEventType_API_AUTH, adapter.WebhookEventType_API_VERIFY_AND_AUTH,
		adapter.WebhookEventType_THREEDS_VERIFY, adapter.WebhookEventType_CHECKOUTFORM_AUTH:
		return providers.WebhookPayment
	case adapter.WebhookEventType_REFUND, adapter.WebhookEventType_REFUND_TX:
		return providers.WebhookRefund
	default:
		return providers.WebhookOther
	}
}

func webhookState(s adapter.WebhookStatus) providers.PaymentState {
	switch s {
	case adapter.WebhookStatus_SUCCESS:
		return providers.StateSucceeded
	case adapter.WebhookStatus_FAILURE:
		return providers.StateFailed
	default:
		return providers.StatePending
	}
}

func brand(association *string) string {
	switch strings.ToUpper(deref(association)) {
	case "VISA":
		return "visa"
	case "MASTER_CARD":
		return "mastercard"
	case "AMEX":
		return "amex"
	case "TROY":
		return "troy"
	case "MAESTRO":
		return "maestro"
	default:
		return strings.ToLower(deref(association))
	}
}

func expiryFrom(data map[string]any) (int, int) {
	month, _ := data["cardExpireMonth"].(string)
	year, _ := data["cardExpireYear"].(string)
	m, errM := strconv.Atoi(month)
	y, errY := strconv.Atoi(year)
	if errM != nil || errY != nil {
		return 0, 0
	}

	return m, y
}

// amount converts at the wire: Craftgate's API takes prices as JSON numbers,
// so the decimal is rounded to cents first and only then becomes a float.
func amount(d decimal.Decimal) float64 {
	return d.Round(2).InexactFloat64()
}

func paymentRef(id *int64) string {
	if id == nil {
		return ""
	}

	return strconv.FormatInt(*id, 10)
}

func asCraftgateError(err error) (*adapter.ErrorResponse, bool) {
	var res *adapter.Response[adapter.ErrorResponse]
	if errors.As(err, &res) && res != nil && res.Errors != nil {
		return res.Errors, true
	}

	return nil, false
}

// describe turns a client error into one that is safe to format: the
// client's own Error() dereferences fields that may be absent.
func (c *Craftgate) describe(err error) error {
	if cgErr, ok := asCraftgateError(err); ok {
		return fmt.Errorf("craftgate %s/%s: %s", deref(cgErr.ErrorGroup), deref(cgErr.ErrorCode), deref(cgErr.ErrorDescription))
	}

	return err
}

func (c *Craftgate) unavailable(step string, err error) error {
	described := c.describe(err)
	c.log.ErrorWithFields(described, logger.NewFields().Add("step", step))

	return fmt.Errorf("payment provider %s: %w", step, described)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
