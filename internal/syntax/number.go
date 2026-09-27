package syntax

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// exactNumberLength bounds the candidates passed to big.ParseFloat, which is
// quadratic in the digit count.
const exactNumberLength = 1024

// numberRepresentable reports whether a lexer numeric candidate is accepted by
// big.ParseFloat at 512 bits, the check upstream cty.ParseNumberVal performs.
func numberRepresentable(text string) bool {
	if len(text) <= exactNumberLength {
		_, _, err := big.ParseFloat(text, 10, 512, big.ToNearestEven)
		return err == nil
	}
	return largeNumberRepresentable(text)
}

// largeNumberRepresentable reproduces numberRepresentable's result in linear
// time. ParseFloat fails on malformed syntax, an exponent outside int64, or a
// binary exponent outside the int32 range. The mantissa's bit length is
// estimated from a float64 logarithm, which is exact unless the digits lie
// within about one part in 10^12 of a power of two.
func largeNumberRepresentable(text string) bool {
	mantissa, exponentText := text, ""
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		mantissa, exponentText = text[:i], text[i+1:]
		if exponentText == "" {
			return false
		}
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	if integer == "" || !allDigits(integer) || !allDigits(fraction) {
		return false
	}

	var exponent int64
	if exponentText != "" {
		digits := exponentText
		if digits[0] == '+' || digits[0] == '-' {
			digits = digits[1:]
		}
		if digits == "" || !allDigits(digits) {
			return false
		}
		var err error
		if exponent, err = strconv.ParseInt(exponentText, 10, 64); err != nil {
			return false
		}
	}

	significant := strings.TrimLeft(integer+fraction, "0")
	if significant == "" {
		return true // zero skips the range check
	}
	binary := decimalBitLength(significant) - int64(len(fraction)) + exponent
	return binary >= big.MinExp && binary <= big.MaxExp
}

// decimalBitLength returns the bit length of a decimal integer with no leading
// zeros, estimated from its first 17 digits: log2(0.d1d2…) + n·log2(10).
func decimalBitLength(digits string) int64 {
	leading, _ := strconv.ParseFloat("0."+digits[:min(len(digits), 17)], 64)
	return int64(math.Floor(math.Log2(leading)+float64(len(digits))*math.Log2(10))) + 1
}

func allDigits(text string) bool {
	for i := range len(text) {
		if !digit(text[i]) {
			return false
		}
	}
	return true
}
