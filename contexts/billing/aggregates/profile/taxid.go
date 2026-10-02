package profile

const (
	tcknLength = 11
	vknLength  = 10
)

// IsValidTCKN checks a Turkish identity number: 11 digits, not starting with
// zero, with the two official check digits. Digit 10 is (7 × Σ odd positions
// − Σ even positions) mod 10 over the first nine digits; digit 11 is the sum
// of the first ten digits mod 10.
func IsValidTCKN(value string) bool {
	digits, ok := digitsOf(value, tcknLength)
	if !ok || digits[0] == 0 {
		return false
	}

	odd := digits[0] + digits[2] + digits[4] + digits[6] + digits[8]
	even := digits[1] + digits[3] + digits[5] + digits[7]
	tenth := ((odd * 7) - even) % 10
	if tenth < 0 {
		tenth += 10
	}
	if digits[9] != tenth {
		return false
	}

	sum := 0
	for _, d := range digits[:10] {
		sum += d
	}

	return digits[10] == sum%10
}

// IsValidVKN checks a Turkish tax number: 10 digits. The check-digit
// algorithm is not applied so a number the tax office issued is never refused
// by a reimplementation of it; the e-invoice issuer validates it for real.
func IsValidVKN(value string) bool {
	_, ok := digitsOf(value, vknLength)

	return ok
}

func digitsOf(value string, length int) ([]int, bool) {
	if len(value) != length {
		return nil, false
	}
	digits := make([]int, 0, length)
	for _, r := range value {
		if r < '0' || r > '9' {
			return nil, false
		}
		digits = append(digits, int(r-'0'))
	}

	return digits, true
}
