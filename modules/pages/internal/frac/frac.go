// Package frac generates fractional index keys for ordering siblings.
//
// A key is an integer part followed by an optional fraction. The first byte of the integer part encodes
// its length ('a' = 1 digit, 'b' = 2 digits, ... 'z' = 26 digits; 'Z' = 1 digit, 'Y' = 2 digits, ...
// 'A' = 26 digits for the negative side), the digits use the base-62 alphabet 0-9A-Za-z in ASCII order.
// Plain byte-wise string comparison is therefore the sibling order. Appending or prepending grows the
// integer part by one digit only after 62^n keys, and inserting between two keys bisects the fraction.
//
// Keys are never empty and never end in the minimum digit '0', so there is always room before any key.
package frac

import (
	"errors"
	"fmt"
	"strings"
)

const (
	digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	base   = len(digits)
	maxLen = 26 // digits in the longest integer part
)

// ErrOrder is returned when a is not strictly smaller than b.
var ErrOrder = errors.New("frac: a must sort before b")

// Valid reports whether k is a well-formed key.
func Valid(k string) bool {
	n := intLen(k)
	if n == 0 || len(k) < n || k[len(k)-1] == '0' {
		return false
	}
	for i := 0; i < len(k); i++ {
		if digitVal(k[i]) < 0 {
			return false
		}
	}
	return true
}

// Between returns a key strictly between a and b. An empty a means "no lower bound" and an empty b "no
// upper bound". It fails when a key is invalid or a >= b.
func Between(a, b string) (string, error) {
	if a != "" && !Valid(a) {
		return "", fmt.Errorf("frac: invalid key %q", a)
	}
	if b != "" && !Valid(b) {
		return "", fmt.Errorf("frac: invalid key %q", b)
	}
	if a != "" && b != "" && a >= b {
		return "", fmt.Errorf("%w: %q, %q", ErrOrder, a, b)
	}
	switch {
	case a == "" && b == "":
		return "a1", nil
	case a == "":
		ib, fb := split(b)
		if fb != "" && ib[len(ib)-1] != '0' {
			return ib, nil
		}
		if fb != "" {
			return ib + midpoint("", fb), nil
		}
		if r, ok := decrement(ib); ok {
			return r, nil
		}
		return "A" + strings.Repeat("0", maxLen) + "V", nil
	case b == "":
		ia, fa := split(a)
		if r, ok := increment(ia); ok {
			return r, nil
		}
		return ia + midpoint(fa, ""), nil
	}
	ia, fa := split(a)
	ib, fb := split(b)
	if ia == ib {
		return ia + midpoint(fa, fb), nil
	}
	if r, ok := increment(ia); ok && r < b {
		return r, nil
	}
	return ia + midpoint(fa, ""), nil
}

// NKeysBetween returns n sorted keys strictly between a and b, spread evenly. Empty a and b are open ends.
func NKeysBetween(a, b string, n int) ([]string, error) {
	if n < 0 {
		return nil, fmt.Errorf("frac: negative key count %d", n)
	}
	if n == 0 {
		return nil, nil
	}
	out := make([]string, n)
	if err := fill(out, a, b); err != nil {
		return nil, err
	}
	return out, nil
}

func fill(out []string, a, b string) error {
	switch {
	case len(out) == 0:
		return nil
	case b == "":
		for i := range out {
			k, err := Between(a, "")
			if err != nil {
				return err
			}
			out[i], a = k, k
		}
	case a == "":
		for i := len(out) - 1; i >= 0; i-- {
			k, err := Between("", b)
			if err != nil {
				return err
			}
			out[i], b = k, k
		}
	default:
		mid := len(out) / 2
		k, err := Between(a, b)
		if err != nil {
			return err
		}
		out[mid] = k
		if err := fill(out[:mid], a, k); err != nil {
			return err
		}
		return fill(out[mid+1:], k, b)
	}
	return nil
}

// intLen returns the length of k's integer part (head included), or 0 for an invalid head.
func intLen(k string) int {
	if k == "" {
		return 0
	}
	switch h := k[0]; {
	case h >= 'a' && h <= 'z':
		return int(h-'a') + 2
	case h >= 'A' && h <= 'Z':
		return int('Z'-h) + 2
	}
	return 0
}

func split(k string) (integer, fraction string) {
	n := intLen(k)
	return k[:n], k[n:]
}

func digitVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 36
	}
	return -1
}

// midpoint returns a fraction strictly between fractions a and b ("" b means 1, "" a means 0). The result
// does not end in '0'. Requires a < b with both free of trailing zeros.
func midpoint(a, b string) string {
	if b != "" {
		n := 0
		for n < len(b) {
			c := byte('0')
			if n < len(a) {
				c = a[n]
			}
			if c != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			if n > len(a) {
				a = ""
			} else {
				a = a[n:]
			}
			return b[:n] + midpoint(a, b[n:])
		}
	}
	da, db := 0, base
	if a != "" {
		da = digitVal(a[0])
	}
	if b != "" {
		db = digitVal(b[0])
	}
	if db-da > 1 {
		return digits[(da+db)/2 : (da+db)/2+1]
	}
	if len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	return digits[da:da+1] + midpoint(rest, "")
}

// increment returns the next integer part whose last digit is not '0'.
func increment(i string) (string, bool) {
	for {
		r, ok := step(i)
		if !ok {
			return "", false
		}
		if r[len(r)-1] != '0' {
			return r, true
		}
		i = r
	}
}

func step(i string) (string, bool) {
	head, ds := i[0], []byte(i[1:])
	j := len(ds) - 1
	for ; j >= 0; j-- {
		if ds[j] != digits[base-1] {
			ds[j] = digits[digitVal(ds[j])+1]
			return string(head) + string(ds), true
		}
		ds[j] = '0'
	}
	switch {
	case head == 'Z':
		return "a0", true
	case head == 'z':
		return "", false
	case head >= 'a':
		return string(head+1) + string(ds) + "0", true
	}
	return string(head+1) + string(ds[:len(ds)-1]), true
}

// decrement returns the previous integer part whose last digit is not '0'.
func decrement(i string) (string, bool) {
	for {
		r, ok := back(i)
		if !ok {
			return "", false
		}
		if r[len(r)-1] != '0' {
			return r, true
		}
		i = r
	}
}

func back(i string) (string, bool) {
	head, ds := i[0], []byte(i[1:])
	for j := len(ds) - 1; j >= 0; j-- {
		if ds[j] != '0' {
			ds[j] = digits[digitVal(ds[j])-1]
			return string(head) + string(ds), true
		}
		ds[j] = digits[base-1]
	}
	switch {
	case head == 'A':
		return "", false
	case head == 'a':
		return "Zz", true
	case head > 'a':
		return string(head-1) + string(ds[:len(ds)-1]), true
	}
	return string(head-1) + string(ds) + "z", true
}
