package constants

type ContextKey string

func (k ContextKey) String() string {
	return string(k)
}

const (
	CorrelationIDHeader = "X-Correlation-ID"
	TargetServerHeader  = "X-Target-Server"

	CorrelationID ContextKey = "correlationID"
	TargetServer  ContextKey = "targetServer"
	TargetHost    ContextKey = "targetHost"
	IsPublic      ContextKey = "isPublic"
	StartTime     ContextKey = "startTime"
	DoNotLog      ContextKey = "doNotLog"

	// The request-auth middleware guarantees these on every authenticated
	// handler; a missing value is broken wiring, not a runtime condition.
	CurrentUserID     ContextKey = "currentUserID"
	CurrentUserEmail  ContextKey = "currentUserEmail"
	CurrentUserStatus ContextKey = "currentUserStatus"
	CurrentSessionID  ContextKey = "currentSessionID"

	// ClientIP and ClientUserAgent describe the device a session is created
	// for; they are read off the request by the API layer.
	ClientIP        ContextKey = "clientIP"
	ClientUserAgent ContextKey = "clientUserAgent"
)
