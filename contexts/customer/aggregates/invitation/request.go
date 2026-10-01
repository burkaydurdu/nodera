package invitation

type CreateRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
	Email  string `json:"email" validate:"required,email,max=255"`
	Role   string `json:"role" validate:"required,role"`
}

func (r *CreateRequest) Normalize() {
	r.Email = NormalizeEmail(r.Email)
}

type ListRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}

// TeamScopedRequest names one Invitation through its Team, so an id learned
// elsewhere cannot be managed through another Team.
type TeamScopedRequest struct {
	TeamID       string `param:"teamID" json:"-" validate:"required,uuid"`
	InvitationID string `param:"invitationID" json:"-" validate:"required,uuid"`
}

type ByIDRequest struct {
	InvitationID string `param:"invitationID" json:"-" validate:"required,uuid"`
}

type TokenRequest struct {
	Token string `json:"token" validate:"required,max=128"`
}

type LookupRequest struct {
	Token string `query:"token" json:"-" validate:"required,max=128"`
}
