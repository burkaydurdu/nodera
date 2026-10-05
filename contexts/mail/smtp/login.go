package smtp

import (
	"errors"

	"github.com/emersion/go-sasl"
)

var errUnexpectedLogin = errors.New("unexpected LOGIN step")

// loginServer is the LOGIN mechanism go-sasl only ships a client for;
// WordPress and many older libraries still prefer it to PLAIN.
type loginServer struct {
	step         int
	username     string
	authenticate func(username, password string) error
}

func newLoginServer(authenticate func(username, password string) error) sasl.Server {
	return &loginServer{authenticate: authenticate}
}

func (s *loginServer) Next(response []byte) ([]byte, bool, error) {
	switch s.step {
	case 0:
		if response != nil {
			s.username, s.step = string(response), 2

			return []byte("Password:"), false, nil
		}
		s.step = 1

		return []byte("Username:"), false, nil
	case 1:
		s.username, s.step = string(response), 2

		return []byte("Password:"), false, nil
	case 2:
		s.step = 3

		return nil, true, s.authenticate(s.username, string(response))
	default:
		return nil, false, errUnexpectedLogin
	}
}
