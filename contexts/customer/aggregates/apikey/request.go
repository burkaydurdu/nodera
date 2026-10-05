package apikey

import "strings"

type CreateRequest struct {
	TeamID      string   `param:"teamID" json:"-" validate:"required,uuid"`
	ProjectID   string   `param:"projectID" json:"-" validate:"required,uuid"`
	Name        string   `json:"name" validate:"required,min=1,max=64"`
	Permissions []string `json:"permissions" validate:"required,min=1,unique,dive,required"`
}

func (r *CreateRequest) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
}

type ListRequest struct {
	TeamID    string `param:"teamID" json:"-" validate:"required,uuid"`
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
}

type RevokeRequest struct {
	TeamID    string `param:"teamID" json:"-" validate:"required,uuid"`
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	KeyID     string `param:"keyID" json:"-" validate:"required,len=12,alphanum"`
}
