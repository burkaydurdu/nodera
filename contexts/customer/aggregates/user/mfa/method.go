package mfa

// Method is the second factor a User presents at sign-in. MethodNone means the
// User turned MFA off.
type Method string

const (
	MethodNone  Method = "none"
	MethodEmail Method = "email"
)

func (m Method) IsValid() bool {
	switch m {
	case MethodNone, MethodEmail:
		return true
	}

	return false
}

func (m Method) String() string {
	return string(m)
}
