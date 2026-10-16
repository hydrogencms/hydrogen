package content

import (
	"cmp"
	"context"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// CheckRelay authorizes relaying an unsaved working document of an item in a locale (the same check as a draft
// update: content.edit on the item). It returns the item's type and the locale ("" means the item's main
// locale). ErrNotFound when the item does not exist or the principal cannot read it.
func (s *Service) CheckRelay(ctx context.Context, id ids.UUID, locale string) (typ, loc string, err error) {
	if err := s.prepare(ctx); err != nil {
		return "", "", err
	}
	err = s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, false)
		if err != nil {
			return err
		}
		loc = cmp.Or(locale, it.MainLocale)
		if err := validateLocale(loc); err != nil {
			return err
		}
		st, err := s.newState(it)
		if err != nil {
			return err
		}
		if err := st.load(ctx, tx.Content()); err != nil {
			return err
		}
		if err := s.check(ctx, access.PolicyContentEdit, st, []string{loc}); err != nil {
			return err
		}
		typ = it.Type
		return nil
	})
	return typ, loc, err
}

// ValidateRelay checks a relayed working document the way a draft save does (types and shapes; required fields
// may be missing), without running hooks or writing anything. It returns the normalized, pruned document, which
// is what a relay stores; fields is not modified. Strings holding U+0000 are refused (the stores cannot keep them).
func (s *Service) ValidateRelay(typ, locale string, fields map[string]any) (map[string]any, error) {
	t, err := s.typeOf(typ)
	if err != nil {
		return nil, err
	}
	doc := cloneDoc(fields)
	s.o.Schema.Normalize(t, doc)
	s.o.Schema.Prune(t, doc)
	errs := s.o.Schema.Validate(t, doc, schema.ModeDraft)
	if path, bad := findNUL("", doc); bad {
		errs = append(errs, schema.FieldError{Path: path, Code: "invalid", Message: "must not contain the character U+0000"})
	}
	if len(errs) > 0 {
		return nil, &ValidationError{Locale: locale, Errors: errs}
	}
	return doc, nil
}

// findNUL reports the path of the first string or key in v holding U+0000.
func findNUL(path string, v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return path, strings.ContainsRune(x, 0)
	case map[string]any:
		for k, e := range x {
			if strings.ContainsRune(k, 0) {
				return path, true
			}
			if p, bad := findNUL(joinPath(path, k), e); bad {
				return p, true
			}
		}
	case []any:
		for i, e := range x {
			if p, bad := findNUL(path+"["+strconv.Itoa(i)+"]", e); bad {
				return p, true
			}
		}
	}
	return "", false
}

func joinPath(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}
