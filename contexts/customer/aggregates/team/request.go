package team

import "strings"

type CreateRequest struct {
	Name string `json:"name" validate:"required,min=1,max=100"`
}

func (r *CreateRequest) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
}

type ListMembersRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}

type ChangeRoleRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
	UserID string `param:"userID" json:"-" validate:"required,uuid"`
	Role   string `json:"role" validate:"required,role"`
}

type RemoveMemberRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
	UserID string `param:"userID" json:"-" validate:"required,uuid"`
}

type LeaveRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}
