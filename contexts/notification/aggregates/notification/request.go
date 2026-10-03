package notification

type ListRequest struct {
	// Before is the id of the last Notification of the previous page.
	Before string `query:"before" json:"-" validate:"omitempty,uuid"`
	Limit  int    `query:"limit" json:"-" validate:"omitempty,min=1,max=50"`
}

func (r *ListRequest) PageSize() int {
	if r.Limit == 0 {
		return DefaultPageSize
	}

	return r.Limit
}

type ReadRequest struct {
	NotificationID string `param:"notificationID" json:"-" validate:"required,uuid"`
}
