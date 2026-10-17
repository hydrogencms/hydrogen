package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Size is a byte count written as a number with an optional unit: "512", "64KB", "50MB", "2GB" (1024-based;
// KiB, MiB and GiB are accepted too). It decodes from a single YAML value.
type Size int64

func (s *Size) UnmarshalText(b []byte) error {
	n, err := ParseSize(string(b))
	*s = Size(n)
	return err
}

// ParseSize parses "50MB"-style sizes.
func ParseSize(v string) (int64, error) {
	s := strings.TrimSpace(v)
	num, unit := s, ""
	if i := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		num, unit = strings.TrimSpace(s[:i]), strings.ToUpper(strings.TrimSpace(s[i:]))
	}
	mult := int64(1)
	switch unit {
	case "", "B":
	case "KB", "KIB", "K":
		mult = 1 << 10
	case "MB", "MIB", "M":
		mult = 1 << 20
	case "GB", "GIB", "G":
		mult = 1 << 30
	default:
		return 0, fmt.Errorf("%q is not a size (e.g. 512KB, 50MB, 2GB)", v)
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil || n < 0 || n > (1<<62)/mult {
		return 0, fmt.Errorf("%q is not a size (e.g. 512KB, 50MB, 2GB)", v)
	}
	return n * mult, nil
}
