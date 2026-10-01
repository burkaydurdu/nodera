package catalog

import "strings"

type ListPlansRequest struct {
	Region string `query:"region" json:"-" validate:"required,max=16"`
}

func (r *ListPlansRequest) Normalize() {
	r.Region = strings.ToLower(strings.TrimSpace(r.Region))
}
