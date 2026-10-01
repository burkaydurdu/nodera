package hash

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// RandomDigits returns a zero-padded numeric code of the given length drawn
// from a cryptographic source.
func RandomDigits(length int) (string, error) {
	limit := big.NewInt(1)
	for i := 0; i < length; i++ {
		limit.Mul(limit, big.NewInt(10))
	}

	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%0*d", length, n.Int64()), nil
}
