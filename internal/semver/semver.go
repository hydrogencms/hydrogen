// Package semver parses semantic versions and version ranges, enough for module compatibility checks. A
// version is MAJOR.MINOR.PATCH with an optional pre-release (-rc.1) and build metadata (+x, ignored), with an
// optional v prefix. A range is comparisons joined by commas, all of which must hold: ">=0.4.0, <0.6.0".
// Operators are = != < <= > >=; a bare version means =. Ordering follows semver precedence, so 0.5.0-rc.1 is
// below 0.5.0.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed semantic version.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string // dot-separated pre-release identifiers
}

// Parse reads a version such as "v1.2.3" or "0.5.0-rc.1+build".
func Parse(s string) (Version, error) {
	orig := s
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, build, hasBuild := strings.Cut(s, "+")
	if hasBuild {
		for _, id := range strings.Split(build, ".") {
			if id == "" || !identifier(id) {
				return Version{}, fmt.Errorf("invalid version %q: bad build metadata %q", orig, build)
			}
		}
	}
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH", orig)
	}
	var v Version
	for i, dst := range []*uint64{&v.Major, &v.Minor, &v.Patch} {
		n, err := number(parts[i])
		if err != nil {
			return Version{}, fmt.Errorf("invalid version %q: %v", orig, err)
		}
		*dst = n
	}
	if hasPre {
		v.Pre = strings.Split(pre, ".")
		for _, id := range v.Pre {
			if id == "" || !identifier(id) {
				return Version{}, fmt.Errorf("invalid version %q: bad pre-release %q", orig, pre)
			}
			if numeric(id) && len(id) > 1 && id[0] == '0' {
				return Version{}, fmt.Errorf("invalid version %q: numeric pre-release identifier %q has a leading zero", orig, id)
			}
		}
	}
	return v, nil
}

func number(s string) (uint64, error) {
	if s == "" || !numeric(s) {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("%q has a leading zero", s)
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is too large", s)
	}
	return n, nil
}

func numeric(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

func identifier(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' {
			return false
		}
	}
	return true
}

// Compare returns -1, 0 or 1 by semver precedence.
func (v Version) Compare(o Version) int {
	for _, p := range [][2]uint64{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if c := cmp(p[0], p[1]); c != 0 {
			return c
		}
	}
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(o.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(o.Pre); i++ {
		if c := compareIdent(v.Pre[i], o.Pre[i]); c != 0 {
			return c
		}
	}
	return cmp(uint64(len(v.Pre)), uint64(len(o.Pre)))
}

func compareIdent(a, b string) int {
	an, bn := numeric(a), numeric(b)
	switch {
	case an && bn:
		// No leading zeros (Parse rejects them), so a longer number is larger; no integer conversion to overflow.
		if len(a) != len(b) {
			return cmp(uint64(len(a)), uint64(len(b)))
		}
		return strings.Compare(a, b)
	case an:
		return -1 // numeric identifiers sort below alphanumeric ones
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

func cmp(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}

type constraint struct {
	op string
	v  Version
}

// Range is a set of comparisons that must all hold.
type Range struct {
	text string
	cs   []constraint
}

// ParseRange reads a range such as ">=0.4.0, <0.6.0". An empty range is an error.
func ParseRange(s string) (Range, error) {
	r := Range{text: strings.TrimSpace(s)}
	if r.text == "" {
		return Range{}, fmt.Errorf("empty version range")
	}
	for _, part := range strings.Split(r.text, ",") {
		part = strings.TrimSpace(part)
		op := "="
		for _, o := range []string{">=", "<=", "!=", ">", "<", "="} {
			if rest, ok := strings.CutPrefix(part, o); ok {
				op, part = o, strings.TrimSpace(rest)
				break
			}
		}
		v, err := Parse(part)
		if err != nil {
			return Range{}, fmt.Errorf("invalid version range %q: %w", s, err)
		}
		r.cs = append(r.cs, constraint{op, v})
	}
	return r, nil
}

// Contains reports whether v satisfies every comparison.
func (r Range) Contains(v Version) bool {
	for _, c := range r.cs {
		n := v.Compare(c.v)
		var ok bool
		switch c.op {
		case "=":
			ok = n == 0
		case "!=":
			ok = n != 0
		case "<":
			ok = n < 0
		case "<=":
			ok = n <= 0
		case ">":
			ok = n > 0
		case ">=":
			ok = n >= 0
		}
		if !ok {
			return false
		}
	}
	return true
}

func (r Range) String() string { return r.text }
