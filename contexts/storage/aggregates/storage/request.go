package storage

import "strings"

type OpenRequest struct {
	ProjectID  string `param:"projectID" json:"-" validate:"required,uuid"`
	BucketName string `json:"bucket_name" validate:"required,max=128"`
	Region     string `json:"region" validate:"required,max=16"`
	// Package is a code of the catalog.
	Package string `json:"package" validate:"required,max=32"`
}

func (r *OpenRequest) Normalize() {
	r.BucketName = strings.TrimSpace(r.BucketName)
	r.Region = strings.ToLower(strings.TrimSpace(r.Region))
	r.Package = strings.ToLower(strings.TrimSpace(r.Package))
}

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
}

type GetRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
}

// DeleteRequest repeats the Bucket name, so that a Storage and everything in
// it is never deleted by a slip.
type DeleteRequest struct {
	ProjectID  string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID  string `param:"storageID" json:"-" validate:"required,uuid"`
	BucketName string `json:"bucket_name" query:"bucket_name" validate:"required,max=63"`
}

func (r *DeleteRequest) Normalize() {
	r.BucketName = strings.TrimSpace(r.BucketName)
}

type ChoosePackageRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
	Code      string `json:"code" validate:"required,max=32"`
}

func (r *ChoosePackageRequest) Normalize() {
	r.Code = strings.ToLower(strings.TrimSpace(r.Code))
}
