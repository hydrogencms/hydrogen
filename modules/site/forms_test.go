package site

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
)

func TestFormGuardToken(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	g := newFormGuard("secret", FormsConfig{Honeypot: "website_url", MinFill: 3 * time.Second, MaxAge: time.Hour}, clk)
	tok := g.token("web")
	if err := g.verify("web", tok); !errors.Is(err, ErrTooFast) {
		t.Errorf("immediately: %v", err)
	}
	clk.Advance(5 * time.Second)
	if err := g.verify("web", tok); err != nil {
		t.Errorf("after min fill: %v", err)
	}
	if err := g.verify("other", tok); !errors.Is(err, ErrFormInvalid) {
		t.Errorf("other channel: %v", err)
	}
	if err := g.verify("web", strings.Replace(tok, ".", "0.", 1)); !errors.Is(err, ErrFormInvalid) {
		t.Errorf("tampered: %v", err)
	}
	other := newFormGuard("another", g.cfg, clk)
	if err := other.verify("web", tok); !errors.Is(err, ErrFormInvalid) {
		t.Errorf("other key: %v", err)
	}
	clk.Advance(2 * time.Hour)
	if err := g.verify("web", tok); !errors.Is(err, ErrFormInvalid) {
		t.Errorf("expired: %v", err)
	}
}

func TestFormsConfigValidate(t *testing.T) {
	ok := FormsConfig{Honeypot: "website_url", MinFill: time.Second, MaxAge: time.Hour}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]FormsConfig{
		"name":     {Honeypot: "Bad Name", MaxAge: time.Hour},
		"reserved": {Honeypot: guardField, MaxAge: time.Hour},
		"age":      {Honeypot: "website_url", MinFill: time.Hour, MaxAge: time.Minute},
	} {
		if err := c.validate(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestLimitValidate(t *testing.T) {
	for name, l := range map[string]Limit{
		"empty":  {Window: time.Minute},
		"window": {PerClient: 1},
		"neg":    {PerClient: -1, Window: time.Minute},
	} {
		if l.validate() == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := (Limit{PerClient: 1, Window: time.Minute}).validate(); err != nil {
		t.Error(err)
	}
	if got := clientBucket("2001:db8:1:2:3:4:5:6"); got != "2001:db8:1:2::/64" {
		t.Errorf("ipv6 bucket = %s", got)
	}
}
