package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/billing/storage"
)

func TestLocal_PutThenGet(t *testing.T) {
	store := storage.NewLocal(t.TempDir())
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "invoices/team/1.pdf", []byte("%PDF")))
	got, err := store.Get(ctx, "invoices/team/1.pdf")

	require.NoError(t, err)
	assert.Equal(t, []byte("%PDF"), got)
}

func TestLocal_MissingIsNotFound(t *testing.T) {
	_, err := storage.NewLocal(t.TempDir()).Get(context.Background(), "invoices/none.pdf")

	assert.ErrorIs(t, err, storage.ErrNotFound)
}

func TestLocal_KeyCannotEscape(t *testing.T) {
	store := storage.NewLocal(t.TempDir())

	assert.ErrorIs(t, store.Put(context.Background(), "../outside.pdf", []byte("x")), storage.ErrBadKey)
	_, err := store.Get(context.Background(), "../../etc/passwd")
	assert.ErrorIs(t, err, storage.ErrBadKey)
}
