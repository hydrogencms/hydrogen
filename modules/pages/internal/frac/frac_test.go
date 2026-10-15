package frac

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	tests := []struct {
		k  string
		ok bool
	}{
		{"a1", true}, {"aV", true}, {"Zz", true}, {"a1V", true}, {"b01", true},
		{"", false}, {"a0", false}, {"a", false}, {"1", false}, {"a1-", false}, {"a10", false}, {"b1", false},
	}
	for _, tt := range tests {
		if got := Valid(tt.k); got != tt.ok {
			t.Errorf("Valid(%q) = %v, want %v", tt.k, got, tt.ok)
		}
	}
}

func TestBetween(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"", "", "a1"},
		{"a1", "", "a2"},
		{"az", "", "b01"},
		{"a1", "a2", "a1V"},
		{"a1", "a3", "a2"},
		{"a1", "", "a2"},
		{"", "a1", "Zz"},
		{"Zz", "a1", "Zz" + "V"},
		{"a1", "a1V", "a1F"},
		{"a1V", "a2", "a1k"},
		{"a1", "a11", "a10V"},
	}
	for _, tt := range tests {
		got, err := Between(tt.a, tt.b)
		if err != nil {
			t.Fatalf("Between(%q,%q): %v", tt.a, tt.b, err)
		}
		if got != tt.want {
			t.Errorf("Between(%q,%q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
		checkIn(t, tt.a, got, tt.b)
	}
}

func TestBetweenErrors(t *testing.T) {
	for _, tt := range [][2]string{{"a2", "a1"}, {"a1", "a1"}, {"a0", ""}, {"", "x"}, {"a1", "a10"}} {
		if k, err := Between(tt[0], tt[1]); err == nil {
			t.Errorf("Between(%q,%q) = %q, want error", tt[0], tt[1], k)
		}
	}
}

func checkIn(t testing.TB, a, k, b string) {
	t.Helper()
	if !Valid(k) {
		t.Fatalf("invalid key %q from (%q,%q)", k, a, b)
	}
	if (a != "" && k <= a) || (b != "" && k >= b) {
		t.Fatalf("%q not in (%q,%q)", k, a, b)
	}
}

func TestAppendPrependStaysShort(t *testing.T) {
	for _, prepend := range []bool{false, true} {
		k, longest := "a1", 0
		for range 10000 {
			a, b := k, ""
			if prepend {
				a, b = "", k
			}
			n, err := Between(a, b)
			if err != nil {
				t.Fatal(err)
			}
			checkIn(t, a, n, b)
			k = n
			longest = max(longest, len(k))
		}
		if longest > 6 {
			t.Errorf("prepend=%v: key length %d > 6 (%q)", prepend, longest, k)
		}
	}
}

func TestRepeatedInsertGrowth(t *testing.T) {
	// Inserting again and again between a and its newest neighbour bisects the gap: base 62 holds about
	// six bisections per character.
	a, b := "a1", "a2"
	for n := 1; n <= 600; n++ {
		k, err := Between(a, b)
		if err != nil {
			t.Fatal(err)
		}
		checkIn(t, a, k, b)
		b = k
		if len(k) > 2+n/5+1 {
			t.Fatalf("after %d insertions key %q is too long", n, k)
		}
	}
}

func TestNKeysBetween(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		n    int
	}{{"", "", 0}, {"", "", 1}, {"", "", 100}, {"a1", "", 70}, {"", "a1", 70}, {"a1", "a2", 200}, {"a1", "a1V", 50}, {"Zz", "a1", 1000}} {
		keys, err := NKeysBetween(tc.a, tc.b, tc.n)
		if err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		if len(keys) != tc.n || !slices.IsSorted(keys) {
			t.Fatalf("%+v: got %d keys, sorted=%v", tc, len(keys), slices.IsSorted(keys))
		}
		prev := tc.a
		for _, k := range keys {
			checkIn(t, prev, k, tc.b)
			prev = k
		}
		if tc.n > 1 && slices.Compact(slices.Clone(keys))[len(keys)-1] != keys[len(keys)-1] {
			t.Fatal("duplicates")
		}
	}
	if _, err := NKeysBetween("a2", "a1", 3); err == nil {
		t.Error("want order error")
	}
	if _, err := NKeysBetween("", "", -1); err == nil {
		t.Error("want error for negative n")
	}
}

func TestNKeysEvenlySpread(t *testing.T) {
	keys, _ := NKeysBetween("", "", 10000)
	for _, k := range keys {
		if len(k) > 4 {
			t.Fatalf("key %q longer than 4", k)
		}
	}
}

func TestRandomInsertions(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	keys := []string{}
	for range 5000 {
		i := r.IntN(len(keys) + 1)
		var a, b string
		if i > 0 {
			a = keys[i-1]
		}
		if i < len(keys) {
			b = keys[i]
		}
		k, err := Between(a, b)
		if err != nil {
			t.Fatal(err)
		}
		checkIn(t, a, k, b)
		keys = slices.Insert(keys, i, k)
	}
	if !slices.IsSorted(keys) {
		t.Fatal("not sorted")
	}
	if m := maxLenOf(keys); m > 12 {
		t.Errorf("max key length %d", m)
	}
}

func maxLenOf(keys []string) int {
	m := 0
	for _, k := range keys {
		m = max(m, len(k))
	}
	return m
}

func FuzzBetween(f *testing.F) {
	for _, s := range [][2]string{{"", ""}, {"a1", ""}, {"", "a1"}, {"a1", "a2"}, {"az", "b01"}, {"Zz", "a1"}, {"a1", "a11"}} {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if (a != "" && !Valid(a)) || (b != "" && !Valid(b)) || (a != "" && b != "" && a >= b) {
			if _, err := Between(a, b); err == nil && (a != "" && !Valid(a) || b != "" && !Valid(b) || a != "" && b != "" && a >= b) {
				t.Fatalf("Between(%q,%q) accepted bad input", a, b)
			}
			return
		}
		k, err := Between(a, b)
		if err != nil {
			t.Fatalf("Between(%q,%q): %v", a, b, err)
		}
		checkIn(t, a, k, b)
		if strings.HasSuffix(k, "0") {
			t.Fatalf("%q ends in 0", k)
		}
	})
}

func BenchmarkBetweenAppend(b *testing.B) {
	k := "a1"
	for range b.N {
		k, _ = Between(k, "")
		if len(k) > 8 {
			k = "a1"
		}
	}
}

func BenchmarkBetweenMiddle(b *testing.B) {
	for range b.N {
		_, _ = Between("a1", "a2")
	}
}

func BenchmarkNKeysBetween1000(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_, _ = NKeysBetween("", "", 1000)
	}
}
