package v1

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/piggystats/nodera/contexts/billing/aggregates/billingstatus"
	"github.com/piggystats/nodera/contexts/billing/aggregates/invoice"
	"github.com/piggystats/nodera/contexts/billing/aggregates/payment"
	"github.com/piggystats/nodera/contexts/billing/aggregates/paymentmethod"
	"github.com/piggystats/nodera/contexts/billing/aggregates/profile"
	"github.com/piggystats/nodera/contexts/billing/aggregates/usage"
	"github.com/piggystats/nodera/contexts/billing/api/v1/middleware"
	"github.com/piggystats/nodera/contexts/billing/application/dto"
)

const (
	// webhookMaxBody bounds what a public caller can make the service read.
	webhookMaxBody = 256 << 10
	mimePDF        = "application/pdf"
	// callbackMaxFields bounds the provider form a public caller can post.
	callbackMaxFields = 32
)

type IProfileHandler interface {
	Get(ctx context.Context, req *profile.GetRequest) (*dto.Profile, error)
	Put(ctx context.Context, req *profile.PutRequest) (*dto.Profile, error)
}

type IPaymentMethodHandler interface {
	List(ctx context.Context, req *paymentmethod.ListRequest) ([]*dto.PaymentMethod, error)
	StartVerification(ctx context.Context, req *paymentmethod.StartVerificationRequest) (*dto.CardVerification, error)
	CompleteVerification(ctx context.Context, req *paymentmethod.CompleteVerificationRequest) (*dto.PaymentMethod, error)
	SetDefault(ctx context.Context, req *paymentmethod.SetDefaultRequest) (*dto.PaymentMethod, error)
	Delete(ctx context.Context, req *paymentmethod.DeleteRequest) error
}

type IStatusHandler interface {
	Get(ctx context.Context, req *billingstatus.GetRequest) (*dto.BillingStatus, error)
}

type IWebhookHandler interface {
	Receive(ctx context.Context, req *payment.WebhookRequest) error
}

type IUsageHandler interface {
	Get(ctx context.Context, req *usage.GetRequest) (*dto.Usage, error)
}

type IInvoiceHandler interface {
	List(ctx context.Context, req *invoice.ListRequest) ([]dto.InvoiceSummary, error)
	Get(ctx context.Context, req *invoice.GetRequest) (*dto.Invoice, error)
	PDF(ctx context.Context, req *invoice.GetRequest) ([]byte, string, error)
	Download(ctx context.Context, req *invoice.DownloadRequest) ([]byte, string, error)
	Pay(ctx context.Context, req *invoice.PayRequest) (*dto.Payment, error)
}

type IPaymentHandler interface {
	Confirm(ctx context.Context, req *payment.ConfirmRequest) (*dto.Payment, error)
	StartConfirmation(ctx context.Context, req *payment.StartConfirmationRequest) (*dto.ConfirmationStart, error)
	Callback(ctx context.Context, req *payment.CallbackRequest) (string, error)
}

// Handlers are the request-side rules the routes delegate to.
type Handlers struct {
	Profiles IProfileHandler
	Methods  IPaymentMethodHandler
	Status   IStatusHandler
	Webhooks IWebhookHandler
	Usage    IUsageHandler
	Invoices IInvoiceHandler
	Payments IPaymentHandler
}

// Billing serves the Team-scoped billing routes and the public ones: the
// provider webhooks, the 3-D Secure confirmation and the signed PDF link.
type Billing struct {
	profiles     IProfileHandler
	methods      IPaymentMethodHandler
	status       IStatusHandler
	webhooks     IWebhookHandler
	usage        IUsageHandler
	invoices     IInvoiceHandler
	payments     IPaymentHandler
	authenticate echo.MiddlewareFunc
}

func NewBilling(h Handlers, authenticate echo.MiddlewareFunc) *Billing {
	return &Billing{
		profiles: h.Profiles, methods: h.Methods, status: h.Status, webhooks: h.Webhooks,
		usage: h.Usage, invoices: h.Invoices, payments: h.Payments, authenticate: authenticate,
	}
}

// TeamRoutes is the authenticated group every Team-scoped billing route
// hangs off; usage, invoices and payments register on it too.
func (h *Billing) TeamRoutes(group *echo.Group) *echo.Group {
	return group.Group("/teams/:teamID/billing", h.authenticate)
}

// PublicRoutes is the group the provider-facing routes hang off.
func (h *Billing) PublicRoutes(group *echo.Group) *echo.Group {
	return group.Group("/billing")
}

func (h *Billing) SetRoutes(group *echo.Group) {
	team := h.TeamRoutes(group)
	team.GET("/profile", h.GetProfile)
	team.PUT("/profile", h.PutProfile)
	team.GET("/payment-methods", h.ListPaymentMethods)
	team.POST("/payment-methods/verify", h.StartVerification)
	team.POST("/payment-methods/verify/complete", h.CompleteVerification)
	team.PUT("/payment-methods/:methodID/default", h.SetDefault)
	team.DELETE("/payment-methods/:methodID", h.DeletePaymentMethod)
	team.GET("/status", h.GetStatus)
	team.GET("/usage", h.GetUsage)
	team.GET("/invoices", h.ListInvoices)
	team.GET("/invoices/:invoiceID", h.GetInvoice)
	team.GET("/invoices/:invoiceID/pdf", h.GetInvoicePDF)
	team.POST("/invoices/:invoiceID/pay", h.PayInvoice)

	public := h.PublicRoutes(group)
	public.POST("/webhooks/craftgate", h.webhook("craftgate"))
	public.POST("/webhooks/stripe", h.webhook("stripe"))
	public.GET("/payments/:paymentID/confirm", h.ConfirmPayment)
	public.POST("/payments/:paymentID/3ds/start", h.StartConfirmation)
	public.POST("/payments/:paymentID/3ds/callback", h.ConfirmationCallback)
	public.GET("/invoices/:invoiceID/pdf", h.DownloadInvoicePDF)
}

// GetProfile godoc
// @Summary The Team's Billing profile
// @Description 404 profile 30-10001 when the Team has none yet. 403 without billing.read; 404 access 37-10001 for a Team the caller is not in.
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {object} dto.Profile
// @Router /v1/teams/{teamID}/billing/profile [get]
func (h *Billing) GetProfile(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req profile.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	p, err := h.profiles.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, p)
}

// PutProfile godoc
// @Summary Create or replace the Team's Billing profile
// @Description TR company needs a 10-digit VKN in tax_id and tax_office; TR individual needs a valid TCKN in tax_id (400 30-10004). Changing the country while cards are saved is 409 30-10003. Needs billing.write.
// @Tags billing
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param request body profile.PutRequest true "Billing profile"
// @Success 200 {object} dto.Profile
// @Router /v1/teams/{teamID}/billing/profile [put]
func (h *Billing) PutProfile(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req profile.PutRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	p, err := h.profiles.Put(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, p)
}

// ListPaymentMethods godoc
// @Summary The Team's saved cards, default first
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {array} dto.PaymentMethod
// @Router /v1/teams/{teamID}/billing/payment-methods [get]
func (h *Billing) ListPaymentMethods(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req paymentmethod.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	methods, err := h.methods.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, methods)
}

// StartVerification godoc
// @Summary Open the hosted card form
// @Description Answers where to send the browser; the form returns to {front}/billing/cards/complete?verification=<id>. 409 31-10002 without a Billing profile. Needs billing.write.
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 201 {object} dto.CardVerification
// @Router /v1/teams/{teamID}/billing/payment-methods/verify [post]
func (h *Billing) StartVerification(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req paymentmethod.StartVerificationRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	started, err := h.methods.StartVerification(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, started)
}

// CompleteVerification godoc
// @Summary Save the card the hosted form verified
// @Description The first card becomes the default and lifts the payment-method block on Droplet creation. 402 31-10005 when the card was not verified; 404 31-10006 for an unknown verification. Needs billing.write.
// @Tags billing
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param request body paymentmethod.CompleteVerificationRequest true "Verification id and the parameters the form returned with"
// @Success 201 {object} dto.PaymentMethod
// @Router /v1/teams/{teamID}/billing/payment-methods/verify/complete [post]
func (h *Billing) CompleteVerification(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req paymentmethod.CompleteVerificationRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	m, err := h.methods.CompleteVerification(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusCreated, m)
}

// SetDefault godoc
// @Summary Make a card the one charged
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Param methodID path string true "Payment method id"
// @Success 200 {object} dto.PaymentMethod
// @Router /v1/teams/{teamID}/billing/payment-methods/{methodID}/default [put]
func (h *Billing) SetDefault(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req paymentmethod.SetDefaultRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	m, err := h.methods.SetDefault(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, m)
}

// DeletePaymentMethod godoc
// @Summary Delete a card
// @Description 409 31-10003 for the default while other cards exist; 409 31-10004 for the last card while an Invoice is unpaid. Needs billing.write.
// @Tags billing
// @Security BearerAuth
// @Param teamID path string true "Team id"
// @Param methodID path string true "Payment method id"
// @Success 204
// @Router /v1/teams/{teamID}/billing/payment-methods/{methodID} [delete]
func (h *Billing) DeletePaymentMethod(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req paymentmethod.DeleteRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	if err := h.methods.Delete(ctx, &req); err != nil {
		return err
	}

	return c.NoContent(http.StatusNoContent)
}

// GetStatus godoc
// @Summary Whether the Team may spend
// @Description Readable by every member (team.read); carries no amounts.
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {object} dto.BillingStatus
// @Router /v1/teams/{teamID}/billing/status [get]
func (h *Billing) GetStatus(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req billingstatus.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	status, err := h.status.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, status)
}

// webhook godoc
// @Summary Payment provider notification
// @Description Public. 401 34-10001 on a bad signature; otherwise 200, stored once per provider event id and reconciled asynchronously.
// @Tags billing
// @Accept json
// @Produce json
// @Success 200 {object} dto.WebhookReceipt
// @Router /v1/billing/webhooks/craftgate [post]
// @Router /v1/billing/webhooks/stripe [post]
func (h *Billing) webhook(provider string) echo.HandlerFunc {
	return func(c echo.Context) error {
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, webhookMaxBody))
		if err != nil {
			return err
		}

		err = h.webhooks.Receive(c.Request().Context(), &payment.WebhookRequest{
			Provider: provider,
			ClientIP: c.RealIP(),
			Headers:  c.Request().Header,
			Body:     body,
		})
		if err != nil {
			return withRetryAfter(c, err)
		}

		return c.JSON(http.StatusOK, dto.WebhookReceipt{Received: true})
	}
}

// GetUsage godoc
// @Summary The open Period's draft Invoice
// @Description Each Droplet's hours not yet on an Invoice this Period (rounded up, capped at 672), priced at its Locked price, and each Project's Mail Package and each Storage's Package: the highest monthly price it held in the Period, less what earlier Invoices of the Period charged for it. A line's kind (droplet, mail or storage) says which fields it carries; the other kinds' are omitted. Tax estimated with the Billing profile's Tax rule (0 without a profile); and the Threshold that issues it early. Needs billing.read.
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {object} dto.Usage
// @Router /v1/teams/{teamID}/billing/usage [get]
func (h *Billing) GetUsage(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req usage.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.usage.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// ListInvoices godoc
// @Summary The Team's Invoices, newest first
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Success 200 {array} dto.InvoiceSummary
// @Router /v1/teams/{teamID}/billing/invoices [get]
func (h *Billing) ListInvoices(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invoice.ListRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.invoices.List(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// GetInvoice godoc
// @Summary One Invoice with its lines, Tax lines and Payments
// @Description A line's kind (droplet, mail or storage) says which fields it carries; the other kinds' are omitted. 404 invoice 33-10001 for an unknown Invoice or another Team's. Needs billing.read.
// @Tags billing
// @Security BearerAuth
// @Produce json
// @Param teamID path string true "Team id"
// @Param invoiceID path string true "Invoice id"
// @Success 200 {object} dto.Invoice
// @Router /v1/teams/{teamID}/billing/invoices/{invoiceID} [get]
func (h *Billing) GetInvoice(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invoice.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.invoices.Get(ctx, &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// GetInvoicePDF godoc
// @Summary The Invoice's PDF
// @Description A statement until an e-invoice issuer is connected. 503 invoice 33-10006 when it cannot be rendered right now. Needs billing.read.
// @Tags billing
// @Security BearerAuth
// @Produce application/pdf
// @Param teamID path string true "Team id"
// @Param invoiceID path string true "Invoice id"
// @Success 200 {file} binary
// @Router /v1/teams/{teamID}/billing/invoices/{invoiceID}/pdf [get]
func (h *Billing) GetInvoicePDF(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invoice.GetRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	data, number, err := h.invoices.PDF(ctx, &req)
	if err != nil {
		return err
	}

	return pdfResponse(c, data, number)
}

// PayInvoice godoc
// @Summary Charge the Invoice now
// @Description With the default card, or payment_method_id for another saved card. 409 invoice 33-10002 when paid or void; 429 payment 34-10003 past 5 attempts an hour per Team (Retry-After set). The answer is the attempt as it stands: succeeded, failed, pending, or confirmation_required with the bank's page. Needs billing.write.
// @Tags billing
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param teamID path string true "Team id"
// @Param invoiceID path string true "Invoice id"
// @Param request body invoice.PayRequest false "Another saved card"
// @Success 202 {object} dto.Payment
// @Router /v1/teams/{teamID}/billing/invoices/{invoiceID}/pay [post]
func (h *Billing) PayInvoice(c echo.Context) error {
	ctx, err := middleware.GetContext(c)
	if err != nil {
		return err
	}

	var req invoice.PayRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.invoices.Pay(ctx, &req)
	if err != nil {
		return withRetryAfter(c, err)
	}

	return c.JSON(http.StatusAccepted, out)
}

// ConfirmPayment godoc
// @Summary The Payment behind a confirmation link, as it stands
// @Description Public: the token from the confirmation email authorizes this one Payment. Answers its status and calls no provider; the 3-D Secure step starts with POST .../3ds/start. 410 payment 34-10002 for an unknown Payment or an invalid or expired token.
// @Tags billing
// @Produce json
// @Param paymentID path string true "Payment id"
// @Param token query string true "Confirmation token"
// @Success 200 {object} dto.Payment
// @Router /v1/billing/payments/{paymentID}/confirm [get]
func (h *Billing) ConfirmPayment(c echo.Context) error {
	var req payment.ConfirmRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	out, err := h.payments.Confirm(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// StartConfirmation godoc
// @Summary Open the bank's 3-D Secure step for a Payment awaiting the cardholder
// @Description Public: the token from the confirmation email authorizes this one Payment. Answers html_content (a full HTML document to render) or redirect_url, never neither. 410 payment 34-10002 for a bad or expired token; 409 34-10009 when the Payment is not awaiting confirmation; 402 34-10010 when the card is declined; 502 34-10008 when the bank's step cannot be opened.
// @Tags billing
// @Produce json
// @Param paymentID path string true "Payment id"
// @Param token query string true "Confirmation token"
// @Success 200 {object} dto.ConfirmationStart
// @Router /v1/billing/payments/{paymentID}/3ds/start [post]
func (h *Billing) StartConfirmation(c echo.Context) error {
	var req payment.StartConfirmationRequest
	if err := bindPathAndQuery(c, &req); err != nil {
		return err
	}

	out, err := h.payments.StartConfirmation(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, out)
}

// ConfirmationCallback godoc
// @Summary Where the payment provider returns the cardholder after 3-D Secure
// @Description Public, posted by the provider's page as a form. The link token and the provider's own hash must both verify (401 payment 34-10011 otherwise); the charge is then completed and the browser redirected to {front}/billing/confirm?payment=<id>&result=succeeded|failed.
// @Tags billing
// @Accept x-www-form-urlencoded
// @Param paymentID path string true "Payment id"
// @Param token query string true "Confirmation token"
// @Success 302
// @Router /v1/billing/payments/{paymentID}/3ds/callback [post]
func (h *Billing) ConfirmationCallback(c echo.Context) error {
	var req payment.CallbackRequest
	if err := bindPathAndQuery(c, &req); err != nil {
		return err
	}
	// The posted fields only: the query carries the link token, which is
	// not the provider's to sign.
	if err := c.Request().ParseForm(); err != nil {
		return err
	}
	form := c.Request().PostForm
	if len(form) > callbackMaxFields {
		return payment.ErrorCallbackNotAuthentic
	}
	req.Callback = make(map[string]string, len(form))
	for key := range form {
		req.Callback[key] = form.Get(key)
	}

	location, err := h.payments.Callback(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return c.Redirect(http.StatusFound, location)
}

// DownloadInvoicePDF godoc
// @Summary The Invoice PDF behind the issue email's link
// @Description Public: expires and token come from the signed link (pdf_url of InvoiceIssued). 410 invoice 33-10005 for a forged, foreign or expired link.
// @Tags billing
// @Produce application/pdf
// @Param invoiceID path string true "Invoice id"
// @Param expires query int true "Link expiry, Unix seconds"
// @Param token query string true "Link signature"
// @Success 200 {file} binary
// @Router /v1/billing/invoices/{invoiceID}/pdf [get]
func (h *Billing) DownloadInvoicePDF(c echo.Context) error {
	var req invoice.DownloadRequest
	if err := bind(c, &req); err != nil {
		return err
	}

	data, number, err := h.invoices.Download(c.Request().Context(), &req)
	if err != nil {
		return err
	}

	return pdfResponse(c, data, number)
}

func pdfResponse(c echo.Context, data []byte, number string) error {
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s.pdf"`, number))

	return c.Blob(http.StatusOK, mimePDF, data)
}
