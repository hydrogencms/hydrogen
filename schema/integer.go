package schema

import (
	"encoding/json"
	"math"
	"strconv"
)

type intKind int

const (
	intOK       intKind = iota // an integer that fits int64
	intFraction                // a number with a fractional part
	intRange                   // an integral number outside int64
	intInvalid                 // not a number
)

const two63 = 9223372036854775808.0 // 2^63

// integerOf reads an integer from any JSON number representation without a float64 round trip: "5.0" and
// "1e3" are integers, "1e19" is out of range and "0.5" has a fraction. neg is the sign of the value.
func integerOf(v any) (n int64, neg bool, k intKind) {
	switch x := v.(type) {
	case json.Number:
		return parseIntegerLiteral(string(x))
	case int:
		return int64(x), x < 0, intOK
	case int32:
		return int64(x), x < 0, intOK
	case int64:
		return x, x < 0, intOK
	case uint64:
		if x > math.MaxInt64 {
			return 0, false, intRange
		}
		return int64(x), false, intOK
	case float32:
		return integerOfFloat(float64(x))
	case float64:
		return integerOfFloat(x)
	}
	return 0, false, intInvalid
}

func integerOfFloat(f float64) (int64, bool, intKind) {
	switch {
	case math.IsNaN(f) || math.IsInf(f, 0):
		return 0, false, intInvalid
	case f != math.Trunc(f):
		return 0, f < 0, intFraction
	case f >= two63 || f < -two63:
		return 0, f < 0, intRange
	}
	return int64(f), f < 0, intOK
}

// parseIntegerLiteral parses a JSON number literal as an exact integer, without ever expanding a large
// exponent.
func parseIntegerLiteral(s string) (n int64, neg bool, k intKind) {
	i := 0
	if i < len(s) && s[i] == '-' {
		neg = true
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	intPart := s[start:i]
	if intPart == "" || (len(intPart) > 1 && intPart[0] == '0') {
		return 0, false, intInvalid
	}
	frac := ""
	if i < len(s) && s[i] == '.' {
		i++
		fs := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		frac = s[fs:i]
		if frac == "" {
			return 0, false, intInvalid
		}
	}
	exp, hugeExp := 0, false
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		esign := 1
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			if s[i] == '-' {
				esign = -1
			}
			i++
		}
		es := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			if exp < 1_000_000 {
				exp = exp*10 + int(s[i]-'0')
			} else {
				hugeExp = true
			}
			i++
		}
		if i == es {
			return 0, false, intInvalid
		}
		exp *= esign
	}
	if i != len(s) {
		return 0, false, intInvalid
	}

	digits := trimLeadingZeros(intPart + frac)
	if digits == "" {
		return 0, false, intOK
	}
	exp10 := exp - len(frac)
	for len(digits) > 0 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		exp10++
	}
	switch {
	case hugeExp && exp > 0:
		return 0, neg, intRange
	case hugeExp || exp10 < 0:
		return 0, neg, intFraction
	case len(digits)+exp10 > 19:
		return 0, neg, intRange
	}
	buf := make([]byte, 0, 20)
	if neg {
		buf = append(buf, '-')
	}
	buf = append(buf, digits...)
	for range exp10 {
		buf = append(buf, '0')
	}
	n, err := strconv.ParseInt(string(buf), 10, 64)
	if err != nil {
		return 0, neg, intRange
	}
	return n, neg, intOK
}

func trimLeadingZeros(s string) string {
	for len(s) > 0 && s[0] == '0' {
		s = s[1:]
	}
	return s
}

// cmpIntFloat compares an exact integer with a bound.
func cmpIntFloat(n int64, f float64) int {
	switch {
	case f >= two63:
		return -1
	case f < -two63:
		return 1
	}
	t := math.Trunc(f)
	ti := int64(t)
	switch {
	case n < ti:
		return -1
	case n > ti:
		return 1
	case f > t:
		return -1
	case f < t:
		return 1
	}
	return 0
}

func (c *numberConfig) validateInteger(v any, r *Report) {
	n, neg, k := integerOf(v)
	switch k {
	case intInvalid:
		typeErr(r, "a number")
	case intFraction:
		typeErr(r, "an integer")
	case intRange:
		switch {
		case !neg && c.max != nil && *c.max < two63:
			r.Fail("max", "must be at most %s", fmtNum(*c.max))
		case neg && c.min != nil && *c.min >= -two63:
			r.Fail("min", "must be at least %s", fmtNum(*c.min))
		default:
			typeErr(r, "a 64-bit integer")
		}
	default:
		if c.min != nil && cmpIntFloat(n, *c.min) < 0 {
			r.Fail("min", "must be at least %s", fmtNum(*c.min))
		}
		if c.max != nil && cmpIntFloat(n, *c.max) > 0 {
			r.Fail("max", "must be at most %s", fmtNum(*c.max))
		}
	}
}

// normalizeInteger rewrites an integral json.Number ("5.0", "1e3", "-0") to its plain form.
func normalizeInteger(v any) (any, bool) {
	x, ok := v.(json.Number)
	if !ok {
		return v, false
	}
	n, _, k := parseIntegerLiteral(string(x))
	if k != intOK {
		return v, false
	}
	if s := strconv.FormatInt(n, 10); s != string(x) {
		return json.Number(s), true
	}
	return v, false
}
