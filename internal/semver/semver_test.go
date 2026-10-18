package semver

import "testing"

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		bad  bool
	}{
		{in: "1.2.3", want: "1.2.3"},
		{in: "v0.5.0", want: "0.5.0"},
		{in: "v0.5.0-rc.1", want: "0.5.0-rc.1"},
		{in: "1.0.0+build.5", want: "1.0.0"},
		{in: "v0.0.0-20261018101500-abcdef123456", want: "0.0.0-20261018101500-abcdef123456"},
		{in: "v2.0.0+incompatible", want: "2.0.0"},
		{in: "1.0.0+a.b-c.01", want: "1.0.0"},
		{in: "1.0.0+", bad: true},
		{in: "1.0.0+a..b", bad: true},
		{in: "1.0.0+a_b", bad: true},
		{in: "1.0.0-rc.1+bad!", bad: true},
		{in: "1.2", bad: true},
		{in: "1.2.3.4", bad: true},
		{in: "01.2.3", bad: true},
		{in: "1.2.x", bad: true},
		{in: "1.2.3-", bad: true},
		{in: "1.2.3-rc..1", bad: true},
		{in: "1.2.3-01", bad: true},
		{in: "", bad: true},
		{in: "devel", bad: true},
	} {
		v, err := Parse(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("Parse(%q) = %v, want error", tc.in, v)
			}
			continue
		}
		if err != nil || v.String() != tc.want {
			t.Errorf("Parse(%q) = %v, %v; want %s", tc.in, v, err, tc.want)
		}
	}
}

func TestCompare(t *testing.T) {
	// Ascending order from the semver specification, plus numeric and alphanumeric identifiers.
	order := []string{"0.9.9", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "10.0.0"}
	for i, a := range order {
		for j, b := range order {
			va, _ := Parse(a)
			vb, _ := Parse(b)
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := va.Compare(vb); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", a, b, got, want)
			}
		}
	}
	big1, _ := Parse("1.0.0-rc.99999999999999999999")
	big2, _ := Parse("1.0.0-rc.100000000000000000000")
	if big1.Compare(big2) != -1 || big2.Compare(big1) != 1 {
		t.Error("over-long numeric identifiers must compare by length, then lexically")
	}
	a, _ := Parse("1.0.0+one")
	b, _ := Parse("v1.0.0+two")
	if a.Compare(b) != 0 {
		t.Error("build metadata must not matter")
	}
}

func TestRange(t *testing.T) {
	for _, tc := range []struct {
		rng     string
		version string
		want    bool
	}{
		{">=0.4.0, <0.6.0", "0.4.0", true},
		{">=0.4.0, <0.6.0", "0.5.9", true},
		{">=0.4.0, <0.6.0", "0.6.0", false},
		{">=0.4.0, <0.6.0", "0.3.9", false},
		{">=0.4.0,<0.6.0", "v0.5.0", true},
		{">= v0.4.0 , < v0.6.0", "0.5.0", true},
		{">=0.4.0, <0.6.0", "0.6.0-rc.1", true}, // precedence only: a pre-release sorts below its release
		{">=0.5.0", "0.5.0-rc.1", false},
		{">0.5.0", "0.5.0", false},
		{">0.5.0", "0.5.1", true},
		{"<=0.5.0", "0.5.0", true},
		{"<=0.5.0", "0.5.1", false},
		{"=0.5.0", "0.5.0", true},
		{"0.5.0", "0.5.0", true},
		{"0.5.0", "0.5.1", false},
		{"!=0.5.0", "0.5.0", false},
		{"!=0.5.0", "0.5.1", true},
		{">=0.4.0, !=0.5.2, <1.0.0", "0.5.2", false},
		{">=0.4.0, !=0.5.2, <1.0.0", "0.5.3", true},
		{">=1.0.0-rc.1", "1.0.0-rc.2", true},
	} {
		r, err := ParseRange(tc.rng)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", tc.rng, err)
		}
		v, err := Parse(tc.version)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Contains(v); got != tc.want {
			t.Errorf("%q contains %s = %v, want %v", tc.rng, tc.version, got, tc.want)
		}
	}
}

func TestParseRangeErrors(t *testing.T) {
	for _, in := range []string{"", "  ", ">=", ">=0.4", ">=0.4.0,", ",<1.0.0", "~0.4.0", "^0.4.0", ">=0.4.0 <1.0.0", "=>0.4.0", ">=latest", "1.x"} {
		if r, err := ParseRange(in); err == nil {
			t.Errorf("ParseRange(%q) = %v, want error", in, r)
		}
	}
}
