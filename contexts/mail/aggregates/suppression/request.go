package suppression

import "strings"

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	Search    string `query:"search" json:"-" validate:"max=254"`
	Cursor    string `query:"cursor" json:"-" validate:"max=200"`
	Limit     int    `query:"limit" json:"-" validate:"omitempty,min=1,max=100"`
}

func (r *ListRequest) Normalize() {
	r.Search = strings.ToLower(strings.TrimSpace(r.Search))
}

type DeleteRequest struct {
	ProjectID     string `param:"projectID" json:"-" validate:"required,uuid"`
	SuppressionID string `param:"suppressionID" json:"-" validate:"required,uuid"`
}
