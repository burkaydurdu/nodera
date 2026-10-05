package packages

import "strings"

type ChooseRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	// Code is a catalog Package, or "none" to cancel.
	Code string `json:"code" validate:"required,max=32"`
}

func (r *ChooseRequest) Normalize() {
	r.Code = strings.ToLower(strings.TrimSpace(r.Code))
}

type OverviewRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
}
