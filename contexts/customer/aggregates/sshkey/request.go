package sshkey

import "strings"

type CreateRequest struct {
	TeamID    string `param:"teamID" json:"-" validate:"required,uuid"`
	Name      string `json:"name" validate:"required,min=1,max=64"`
	PublicKey string `json:"public_key" validate:"required"`
}

func (r *CreateRequest) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.PublicKey = strings.TrimSpace(r.PublicKey)
}

type ListRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}

type DeleteRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
	KeyID  string `param:"keyID" json:"-" validate:"required,uuid"`
}
