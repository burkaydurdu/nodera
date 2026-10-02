package paymentmethod

type ListRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}

type StartVerificationRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}

// CompleteVerificationRequest carries whatever the provider appended to the
// return URL; each adapter reads the keys it knows.
type CompleteVerificationRequest struct {
	TeamID         string            `param:"teamID" json:"-" validate:"required,uuid"`
	VerificationID string            `json:"verification_id" validate:"required,uuid"`
	Callback       map[string]string `json:"callback" validate:"max=32,dive,keys,max=64,endkeys,max=4096"`
}

type SetDefaultRequest struct {
	TeamID   string `param:"teamID" json:"-" validate:"required,uuid"`
	MethodID string `param:"methodID" json:"-" validate:"required,uuid"`
}

type DeleteRequest struct {
	TeamID   string `param:"teamID" json:"-" validate:"required,uuid"`
	MethodID string `param:"methodID" json:"-" validate:"required,uuid"`
}
