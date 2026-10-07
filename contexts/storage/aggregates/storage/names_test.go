package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
)

// The rules are an input matrix: each row is one way a name passes or fails.
func TestValidateBucketName(t *testing.T) {
	cases := map[string]error{
		"abc":              nil,
		"a1b":              nil,
		"my-shop-2026":     nil,
		"0start-with-zero": nil,
		"ab":               storage.ErrorInvalidBucketName,
		"Shop":             storage.ErrorInvalidBucketName,
		"shop_assets":      storage.ErrorInvalidBucketName,
		"-shop":            storage.ErrorInvalidBucketName,
		"shop-":            storage.ErrorInvalidBucketName,
		"shop.assets":      storage.ErrorInvalidBucketName,
		"shop assets":      storage.ErrorInvalidBucketName,
		"çay":              storage.ErrorInvalidBucketName,
		"abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0":  nil,
		"abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz01": storage.ErrorInvalidBucketName,
		"admin":        storage.ErrorReservedBucketName,
		"www":          storage.ErrorReservedBucketName,
		"nodera":       storage.ErrorReservedBucketName,
		"nodera-files": storage.ErrorReservedBucketName,
		"xn--abc":      storage.ErrorReservedBucketName,
		"adminx":       nil,
	}
	for name, expected := range cases {
		assert.Equalf(t, expected, storage.ValidateBucketName(name), "%q", name)
	}
}
