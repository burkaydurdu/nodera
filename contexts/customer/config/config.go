package config

import "time"

// Auth is the slice of the binary's config the customer context's API and
// handlers need; the binary's own config package maps its TOML onto it.
type Auth struct {
	JWTSecret  string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	// DevCode replaces every generated code in the dev environment.
	DevCode string
	// FrontURL is where the password reset link points.
	FrontURL string
}
