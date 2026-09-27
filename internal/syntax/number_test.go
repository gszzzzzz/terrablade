package syntax

import (
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

func parseFloatAccepts(text string) bool {
	_, _, err := big.ParseFloat(text, 10, 512, big.ToNearestEven)
	return err == nil
}

func TestLargeNumberMatchesParseFloat(t *testing.T) {
	cases := []string{
		"0", "7", "1.", "1.e2", "1.5", "1e5", "1E+5", "1e-5", "00012.3400e0",
		"1e", "1e+", "1e-", "1e+-5", "1.0.2", "1..2", "1e1e2", "1e2.5", "1.e",
		"0e9223372036854775807", "0e9223372036854775808", "0e+9223372036854775808",
		"0e-9223372036854775808", "0e-9223372036854775809", "000.000e-99999999999",
		"1e2147483583", "1e2147483584", "1e-2147483648", "1e-2147483649",
		"1e9223372036854775807", "1e-9223372036854775808",
		"1" + strings.Repeat("0", 3000),
		"0." + strings.Repeat("0", 3000) + "1",
	}

	// Random significands placed on both sides of the int32 binary-exponent
	// limits, where the bit-length estimate decides the result.
	random := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		digits := make([]byte, 1+random.IntN(1500))
		for i := range digits {
			digits[i] = byte('0' + random.IntN(10))
		}
		digits[0] = byte('1' + random.IntN(9))
		value, _ := new(big.Int).SetString(string(digits), 10)
		fractionDigits := random.IntN(len(digits))
		integer, fraction := string(digits[:len(digits)-fractionDigits]), string(digits[len(digits)-fractionDigits:])
		bitLength := int64(value.BitLen())
		for _, target := range []int64{big.MaxExp, big.MaxExp + 1, big.MinExp, big.MinExp - 1} {
			exponent := target - bitLength + int64(fractionDigits)
			cases = append(cases, integer+"."+fraction+"e"+strconv.FormatInt(exponent, 10))
		}
	}

	// Arbitrary strings over the lexer's candidate alphabet.
	const alphabet = "0123456789..eE+-"
	for range 2000 {
		text := []byte{byte('0' + random.IntN(10))}
		for range random.IntN(12) {
			text = append(text, alphabet[random.IntN(len(alphabet))])
		}
		cases = append(cases, string(text))
	}

	for _, text := range cases {
		if got, want := largeNumberRepresentable(text), parseFloatAccepts(text); got != want {
			t.Errorf("largeNumberRepresentable(%.60q) = %v, big.ParseFloat accepts %v", text, got, want)
		}
	}
}

func TestLongNumberLiteralsParse(t *testing.T) {
	digits := strings.Repeat("9", 1<<20)
	for _, test := range []struct {
		source string
		valid  bool
	}{
		{"a = " + digits + "\n", true},
		{"a = 0." + digits + "e-5\n", true},
		{"a = " + digits + "e2147483647\n", false},
	} {
		diagnostics := Parse([]byte(test.source)).Diagnostics()
		if valid := len(diagnostics) == 0; valid != test.valid {
			t.Errorf("%d-byte literal: diagnostics = %v, want valid %v", len(test.source), diagnostics, test.valid)
		}
	}
}
