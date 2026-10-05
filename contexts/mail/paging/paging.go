// Package paging is the keyset position every list of the mail context pages
// by, newest first, and the opaque form a client hands back for the next page.
package paging

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Cursor is where a page ended: the list goes on below it.
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

func (c Cursor) Encode() string {
	return base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "%d:%s", c.At.UnixNano(), c.ID))
}

// Decode reads a cursor a list handed out, and false for anything else.
func Decode(raw string) (*Cursor, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, false
	}
	nanos, id, found := strings.Cut(string(decoded), ":")
	if !found {
		return nil, false
	}
	at, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return nil, false
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, false
	}

	return &Cursor{At: time.Unix(0, at).UTC(), ID: parsed}, true
}
