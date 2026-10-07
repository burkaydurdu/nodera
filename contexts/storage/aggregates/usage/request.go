package usage

type Request struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
}
