package droplet

import "strings"

const (
	MinSSHKeys = 1
	MaxSSHKeys = 10
)

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
}

type SSHKeyInput struct {
	Name      string `json:"name" validate:"required,max=64"`
	PublicKey string `json:"public_key" validate:"required,max=8192"`
}

// CreateRequest carries codes, not ids: the customer picked them from the
// catalog. The hostname is an RFC 1123 label.
type CreateRequest struct {
	ProjectID string        `param:"projectID" json:"-" validate:"required,uuid"`
	Plan      string        `json:"plan" validate:"required,max=64"`
	Region    string        `json:"region" validate:"required,max=16"`
	Image     string        `json:"image" validate:"required,max=64"`
	Hostname  string        `json:"hostname" validate:"required,min=1,max=63,regex=^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"`
	SSHKeys   []SSHKeyInput `json:"ssh_keys" validate:"required,min=1,max=10,dive"`
}

func (r *CreateRequest) Normalize() {
	r.Plan = strings.ToLower(strings.TrimSpace(r.Plan))
	r.Region = strings.ToLower(strings.TrimSpace(r.Region))
	r.Image = strings.ToLower(strings.TrimSpace(r.Image))
	r.Hostname = strings.ToLower(strings.TrimSpace(r.Hostname))
	for i := range r.SSHKeys {
		r.SSHKeys[i].Name = strings.TrimSpace(r.SSHKeys[i].Name)
		r.SSHKeys[i].PublicKey = strings.TrimSpace(r.SSHKeys[i].PublicKey)
	}
}

type GetRequest struct {
	DropletID string `param:"dropletID" json:"-" validate:"required,uuid"`
}

type ActionRequest struct {
	DropletID string `param:"dropletID" json:"-" validate:"required,uuid"`
	Type      string `json:"type" validate:"required,oneof=power_on power_off reboot"`
}

type DeleteRequest struct {
	DropletID string `param:"dropletID" json:"-" validate:"required,uuid"`
}
