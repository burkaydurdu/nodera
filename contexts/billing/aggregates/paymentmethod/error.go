package paymentmethod

import (
	"net/http"

	"github.com/piggystats/rerror"
)

const Domain rerror.Domain = 31

var (
	// ErrorNotFound also answers a card of another Team.
	ErrorNotFound        = rerror.Error{Code: 10001, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "payment method not found"}
	ErrorProfileRequired = rerror.Error{Code: 10002, Domain: Domain, HttpStatus: http.StatusConflict, Message: "billing profile required"}
	// ErrorDefaultInUse: pick another default first so a Team with cards is
	// never left without one to charge.
	ErrorDefaultInUse = rerror.Error{Code: 10003, Domain: Domain, HttpStatus: http.StatusConflict, Message: "cannot delete the default payment method while others exist"}
	// ErrorLastCardOwed: a Team cannot walk away from a debt by removing its
	// last card.
	ErrorLastCardOwed      = rerror.Error{Code: 10004, Domain: Domain, HttpStatus: http.StatusConflict, Message: "the last payment method cannot be deleted while an invoice is unpaid"}
	ErrorVerificationFail  = rerror.Error{Code: 10005, Domain: Domain, HttpStatus: http.StatusPaymentRequired, Message: "card verification failed"}
	ErrorUnknownVerify     = rerror.Error{Code: 10006, Domain: Domain, HttpStatus: http.StatusNotFound, Message: "unknown card verification"}
	ErrorCannotStore       = rerror.Error{Code: 10007, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "payment method could not be stored"}
	ErrorCannotRead        = rerror.Error{Code: 10008, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "payment method could not be read"}
	ErrorNoRoute           = rerror.Error{Code: 10009, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "no payment route for the billing country"}
	ErrorProviderNotConfig = rerror.Error{Code: 10010, Domain: Domain, HttpStatus: http.StatusInternalServerError, Message: "payment provider is not configured"}
	// ErrorProviderUnavailable is the customer-safe face of any provider
	// failure; the raw cause is logged, never answered.
	ErrorProviderUnavailable = rerror.Error{Code: 10011, Domain: Domain, HttpStatus: http.StatusBadGateway, Message: "the payment service is temporarily unavailable"}
)
