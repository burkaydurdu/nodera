package message

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const returnPathPrefix = "b+"

// tagBytes is how much of the MAC a return path carries: 48 bits cost a
// forger one email per guess.
const tagBytes = 6

// ReturnPaths makes and reads the envelope sender each recipient's copy of a
// Message carries, b+<message id>.<position>.<tag>@<host>. An asynchronous
// bounce comes back to it with nothing else of the Message (VERP): the
// position names the recipient even when the bounce reports a forwarded
// address, and the tag, keyed by Nodera, keeps whoever has seen one return
// path from making up another recipient's.
type ReturnPaths struct {
	key []byte
}

func NewReturnPaths(key []byte) ReturnPaths {
	return ReturnPaths{key: key}
}

func (p ReturnPaths) For(messageID uuid.UUID, position int, host string) string {
	return returnPathPrefix + messageID.String() + "." + strconv.Itoa(position) + "." + p.tag(messageID, position) + "@" + host
}

// Parse answers the Message and the recipient's position a return path
// names, and false for anything Nodera did not make.
func (p ReturnPaths) Parse(address string) (uuid.UUID, int, bool) {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return uuid.Nil, 0, false
	}
	local, found := strings.CutPrefix(strings.ToLower(address[:at]), returnPathPrefix)
	if !found {
		return uuid.Nil, 0, false
	}
	parts := strings.Split(local, ".")
	if len(parts) != 3 {
		return uuid.Nil, 0, false
	}

	id, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, 0, false
	}
	position, err := strconv.Atoi(parts[1])
	if err != nil || position < 0 || strconv.Itoa(position) != parts[1] {
		return uuid.Nil, 0, false
	}
	if !hmac.Equal([]byte(parts[2]), []byte(p.tag(id, position))) {
		return uuid.Nil, 0, false
	}

	return id, position, true
}

func (p ReturnPaths) tag(messageID uuid.UUID, position int) string {
	mac := hmac.New(sha256.New, p.key)
	mac.Write([]byte(messageID.String() + "." + strconv.Itoa(position)))

	return hex.EncodeToString(mac.Sum(nil)[:tagBytes])
}
