package usage

type GetRequest struct {
	TeamID string `param:"teamID" json:"-" validate:"required,uuid"`
}
