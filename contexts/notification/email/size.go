package email

import (
	"strconv"
	"strings"
)

// sizeUnits are decimal, as Storage's Packages are sold: 1 GB is
// 1,000,000,000 bytes.
var sizeUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}

// humanSize prints bytes in the largest unit they fill, with at most one
// decimal, truncated rather than rounded so a Storage short of a mark is never
// shown at it. Turkish writes the decimal with a comma.
func humanSize(bytes int64, lang string) string {
	if bytes < 0 {
		bytes = 0
	}
	unit, i := int64(1), 0
	for i < len(sizeUnits)-1 && bytes >= unit*1000 {
		unit *= 1000
		i++
	}
	tenths := bytes * 10 / unit
	out := strconv.FormatInt(tenths/10, 10)
	if frac := tenths % 10; frac != 0 {
		separator := "."
		if lang == "tr" {
			separator = ","
		}
		out += separator + strconv.FormatInt(frac, 10)
	}

	return strings.Join([]string{out, sizeUnits[i]}, " ")
}
