package invoice

type ListRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}

type GetRequest struct {
	TeamID    string `param:"teamID" json:"-" validate:"required,uuid"`
	InvoiceID string `param:"invoiceID" json:"-" validate:"required,uuid"`
}

// PayRequest charges the Invoice now, with another saved card when
// PaymentMethodID is set and the default card otherwise.
type PayRequest struct {
	TeamID          string `param:"teamID" json:"-" validate:"required,uuid"`
	InvoiceID       string `param:"invoiceID" json:"-" validate:"required,uuid"`
	PaymentMethodID string `json:"payment_method_id" validate:"omitempty,uuid"`
}

// DownloadRequest is the public, signed PDF link the issue email carries.
type DownloadRequest struct {
	InvoiceID string `param:"invoiceID" json:"-" validate:"required,uuid"`
	Expires   int64  `query:"expires" json:"-" validate:"required,gt=0"`
	Token     string `query:"token" json:"-" validate:"required,hexadecimal,max=128"`
}
