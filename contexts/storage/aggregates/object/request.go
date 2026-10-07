package object

type ListRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
	// Prefix is the folder, ending in "/" or empty for the root.
	Prefix string `query:"prefix" validate:"max=1024"`
	Cursor string `query:"cursor" validate:"max=2048"`
}

type DeleteRequest struct {
	ProjectID string   `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string   `param:"storageID" json:"-" validate:"required,uuid"`
	Keys      []string `json:"keys" validate:"required,min=1,max=1000,dive,required,max=1024"`
}

// URLRequest asks for a presigned URL for one object key. ContentType is the
// type the browser will send with an upload; it is signed into the URL and
// means nothing for a download.
type URLRequest struct {
	ProjectID   string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID   string `param:"storageID" json:"-" validate:"required,uuid"`
	Key         string `json:"key" validate:"required,max=1024"`
	ContentType string `json:"content_type" validate:"max=255"`
}

// AccessRequest reads whether anyone may read an object.
type AccessRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
	Key       string `query:"key" validate:"required,max=1024"`
}

// SetAccessRequest lets anyone read an object, or takes that back.
type SetAccessRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
	Key       string `json:"key" validate:"required,max=1024"`
	Public    bool   `json:"public"`
}

type BrowserAccessRequest struct {
	ProjectID string `param:"projectID" json:"-" validate:"required,uuid"`
	StorageID string `param:"storageID" json:"-" validate:"required,uuid"`
}
