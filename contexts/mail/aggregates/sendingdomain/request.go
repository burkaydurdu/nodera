package sendingdomain

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
}

type AddRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	Name      string `json:"name" validate:"required,max=253"`
}

type GetRequest struct {
	ProjectID       string `param:"projectID" json:"-" validate:"required,uuid"`
	SendingDomainID string `param:"domainID" json:"-" validate:"required,uuid"`
}

// KeyRequest is KumoMTA asking for the key to sign a Message with.
type KeyRequest struct {
	SendingDomainID string `param:"domainID" json:"-" validate:"required,uuid"`
}
