package outbox

import (
	"testing"
	"time"
)

func TestMatches(t *testing.T) {
	for _, tc := range []struct {
		topics []string
		topic  string
		want   bool
	}{
		{[]string{"item.published"}, "item.published", true},
		{[]string{"item.*"}, "item.published", true},
		{[]string{"item.*"}, "items.published", false},
		{[]string{"item.*"}, "item", false},
		{[]string{"*"}, "anything", true},
		{[]string{"page.moved", "item.*"}, "page.moved", true},
		{[]string{"page.moved"}, "page.created", false},
	} {
		if got := matches(tc.topics, tc.topic); got != tc.want {
			t.Errorf("matches(%v, %q) = %v", tc.topics, tc.topic, got)
		}
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	for i, w := range want {
		if got := Backoff(i + 1); got != w {
			t.Errorf("Backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	if Backoff(50) != 5*time.Minute || Backoff(0) != time.Second {
		t.Errorf("Backoff bounds: %v %v", Backoff(50), Backoff(0))
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := New(nil, nil, nil, []Consumer{{Name: "a", Topics: []string{"x"}}}, Options{}); err == nil {
		t.Error("consumer without a handler accepted")
	}
}
