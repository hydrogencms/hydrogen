package release

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
)

func TestNormalizeItem(t *testing.T) {
	id := ids.NewGenerator(clock.NewManual(time.Now()), nil).New()
	in, err := normalizeItem(ItemInput{Item: id, Locales: []string{"fr", "en", "fr"}})
	if err != nil || in.Action != ActionPublish || len(in.Locales) != 2 || in.Locales[0] != "en" {
		t.Fatalf("normalize = %+v, %v", in, err)
	}
	for _, bad := range []ItemInput{{Item: id, Action: "burn"}, {Item: id, Action: ActionUnpublish, Locales: []string{"en"}},
		{Item: id, Locales: []string{"not a locale"}}, {Action: ActionPublish}} {
		if _, err := normalizeItem(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		code string
		soft bool
	}{
		{&content.ValidationError{Errors: []schema.FieldError{{Path: "title", Message: "required"}}}, "invalid", true},
		{&content.ConflictError{Parts: []string{"en"}}, "conflict", true},
		{fmt.Errorf("x: %w: nothing to publish", content.ErrBadRequest), "nothing_to_publish", true},
		{fmt.Errorf("%w: policy", access.ErrForbidden), "forbidden", true},
		{fmt.Errorf("wrapped: %w", content.ErrNotFound), "not_found", true},
		{errors.New("connection reset"), "internal", false},
	}
	for _, c := range cases {
		got, soft := classify(c.err)
		if got.Code != c.code || soft != c.soft {
			t.Errorf("classify(%v) = %s soft=%v, want %s soft=%v", c.err, got.Code, soft, c.code, c.soft)
		}
	}
	if e, _ := classify(&content.ValidationError{Errors: []schema.FieldError{{Path: "title", Message: "required"}}}); e.Path != "title" {
		t.Errorf("path = %q", e.Path)
	}
}
