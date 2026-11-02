// Package money implements fixed-point arithmetic for cents, basis points and
// FX rates. All intermediate products go through math/big so no input in range
// can overflow, and rounding is always half-away-from-zero.
package money

import (
	"fmt"
	"math/big"
)

// MaxCents bounds any single amount we accept (¥ 10 万亿), far inside int64.
const MaxCents int64 = 1_000_000_000_000_000

// MulDiv returns round(a*b/c) with half-away-from-zero rounding. c must be > 0.
func MulDiv(a, b, c int64) int64 {
	if c <= 0 {
		panic("money.MulDiv: non-positive divisor")
	}
	num := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	return roundDiv(num, big.NewInt(c))
}

// MulMulDiv returns round(a*b*c/d).
func MulMulDiv(a, b, c, d int64) int64 {
	if d <= 0 {
		panic("money.MulMulDiv: non-positive divisor")
	}
	num := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	num.Mul(num, big.NewInt(c))
	return roundDiv(num, big.NewInt(d))
}

func roundDiv(num, den *big.Int) int64 {
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	// |2r| >= |den| → round away from zero
	twoR := new(big.Int).Abs(r)
	twoR.Lsh(twoR, 1)
	if twoR.Cmp(new(big.Int).Abs(den)) >= 0 {
		if num.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	if !q.IsInt64() {
		panic("money: result overflows int64")
	}
	return q.Int64()
}

// ForeignToCNY converts foreign minor units at rateBps (1 unit = rateBps/10000 CNY).
func ForeignToCNY(foreignCents, rateBps int64) int64 { return MulDiv(foreignCents, rateBps, 10_000) }

// Pct applies a rate in basis points (e.g. 500 = 5%).
func Pct(cents, bps int64) int64 { return MulDiv(cents, bps, 10_000) }

// SimpleInterest = principal × annualBps × days / (10000 × 365).
func SimpleInterest(principalCents, annualBps, days int64) int64 {
	return MulMulDiv(principalCents, annualBps, days, 10_000*365)
}

// Add returns a+b and an error on overflow.
func Add(a, b int64) (int64, error) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, fmt.Errorf("money: overflow %d + %d", a, b)
	}
	return s, nil
}

// Yuan formats cents as "1234.56".
func Yuan(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// YuanGrouped formats cents as "1,234.56".
func YuanGrouped(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	s := fmt.Sprintf("%d", cents/100)
	var out []byte
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return fmt.Sprintf("%s%s.%02d", sign, out, cents%100)
}

// Rate formats bps as a 4-decimal FX rate, e.g. 71234 → "7.1234".
func Rate(bps int64) string { return fmt.Sprintf("%d.%04d", bps/10_000, bps%10_000) }
