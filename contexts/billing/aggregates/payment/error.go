package payment

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 34

var (
	ErrorWebhookSignature = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "webhook signature is invalid"}
	// ErrorConfirmTokenInvalid answers the public confirm route for a token
	// that is unknown, used or expired (contract: 410 34-10002).
	ErrorConfirmTokenInvalid = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusGone, Message: "payment confirmation link is invalid or expired"}
	// ErrorPayRateLimited bounds "Pay now" to 5 attempts per hour per Team
	// (contract: 429 34-10003).
	ErrorPayRateLimited = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusTooManyRequests, Message: "too many payment attempts; try again later"}
	ErrorCannotStore    = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "payment could not be stored"}
	ErrorCannotRead     = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "payment could not be read"}
	ErrorUnknownWebhook = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "unknown webhook source"}
	ErrorNotFound       = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "payment not found"}
	// ErrorProviderUnavailable is the customer-safe face of a provider that
	// did not answer; the raw cause is logged, never answered.
	ErrorProviderUnavailable = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusBadGateway, Message: "the payment service is temporarily unavailable"}
	// ErrorNotAwaitingConfirmation: the attempt was settled or never needed
	// the cardholder.
	ErrorNotAwaitingConfirmation = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the payment is not awaiting confirmation"}
	ErrorConfirmationDeclined    = rerror.Error{Code: 10010, Domain: Domain, HttpStatus: http.StatusPaymentRequired, Message: "the card was declined"}
	// ErrorCallbackNotAuthentic answers a 3-D Secure callback whose hash does
	// not verify or that names another payment.
	ErrorCallbackNotAuthentic = rerror.Error{Code: 10011, Domain: Domain, HttpStatus: http.StatusUnauthorized, Message: "payment confirmation callback is not authentic"}
)
