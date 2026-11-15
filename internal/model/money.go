package model

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxCents bounds every amount blast accepts ($1 billion), which keeps the
// percent arithmetic far away from int64 overflow.
const MaxCents = 1_000_000_000_00

// ParseHundredths reads a plain decimal with at most two fractional digits,
// such as "29", "29.9" or "-12.50", as a whole number of hundredths. It is how
// dollars become cents and percents become basis points without a float in
// between.
func ParseHundredths(s string) (int64, error) {
	digits, negative := strings.CutPrefix(s, "-")
	whole, frac, hasPoint := strings.Cut(digits, ".")
	if whole == "" || !allDigits(whole) || !allDigits(frac) || (hasPoint && frac == "") {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if len(frac) > 2 {
		return 0, fmt.Errorf("%q has more than two decimal places", s)
	}
	n, err := strconv.ParseInt(whole+(frac + "00")[:2], 10, 64)
	if err != nil || n > MaxCents {
		return 0, fmt.Errorf("%q is too large", s)
	}
	if negative {
		n = -n
	}
	return n, nil
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Dollars formats cents for people: "$5,229.00", "-$644.40".
func Dollars(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s$%s.%02d", sign, Thousands(cents/100), cents%100)
}

// Thousands formats a whole number with separators: "12,000".
func Thousands(n int64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	b.WriteString(sign)
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return b.String()
}

// Decimal formats hundredths with no more digits than needed: 2000 is "20",
// 1250 is "12.5". It is the inverse of ParseHundredths.
func Decimal(hundredths int64) string {
	sign := ""
	if hundredths < 0 {
		sign, hundredths = "-", -hundredths
	}
	s := fmt.Sprintf("%s%d.%02d", sign, hundredths/100, hundredths%100)
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}
