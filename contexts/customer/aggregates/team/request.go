package team

import "strings"

type CreateRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
}

func (r *CreateRequest) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
}
