package paging_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/paging"
)

func TestCursor_RoundTrips(t *testing.T) {
	c := paging.Cursor{At: time.Date(2026, 10, 5, 12, 0, 0, 123456000, time.UTC), ID: uuid.New()}

	decoded, ok := paging.Decode(c.Encode())

	require.True(t, ok)
	assert.True(t, c.At.Equal(decoded.At))
	assert.Equal(t, c.ID, decoded.ID)
}

func TestDecode_RefusesWhatNoCursorEncodes(t *testing.T) {
	for _, raw := range []string{"garbage", "", "MTIz", "YWJjOmRlZg"} {
		_, ok := paging.Decode(raw)
		assert.Falsef(t, ok, "%q", raw)
	}
}
