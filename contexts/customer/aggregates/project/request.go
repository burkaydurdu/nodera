package project

import "strings"

type CreateRequest struct {
	TeamID      string `param:"teamID" json:"-" validate:"required,uuid"`
	Name        string `json:"name" validate:"required,min=1,max=100"`
	Description string `json:"description" validate:"max=500"`
}

func (r *CreateRequest) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
}

type ListRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}
