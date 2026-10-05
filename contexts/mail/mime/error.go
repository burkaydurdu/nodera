package mime

import "errors"

// ErrNoFrom is a Message nobody can be told the sender of.
var ErrNoFrom = errors.New("the message has no From header")
