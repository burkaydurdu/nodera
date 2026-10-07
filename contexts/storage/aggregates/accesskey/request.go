package accesskey

import "strings"

type CreateRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
	Name      string `json:"name" validate:"required,max=64"`
	// Permission is read or read_write.
	Permission string `json:"permission" validate:"required,oneof=read read_write"`
}

func (r *CreateRequest) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.Permission = strings.ToLower(strings.TrimSpace(r.Permission))
}

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
}

type RevokeRequest struct {
	ProjectID   string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID   string `param:"storageID" json:"-" validate:"required,uuid"`
	AccessKeyID string `param:"accessKeyID" json:"-" validate:"required,uuid"`
}
