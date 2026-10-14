package schema

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"
)

// Error is one problem found while loading or compiling schema definitions.
type Error struct {
	Pos     Pos
	Message string
}

func (e *Error) Error() string {
	if e.Pos.File == "" {
		return e.Message
	}
	return e.Pos.String() + ": " + e.Message
}

type errList struct {
	list []*Error
}

func (l *errList) add(pos Pos, format string, args ...any) {
	l.list = append(l.list, &Error{Pos: pos, Message: fmt.Sprintf(format, args...)})
}

func (l *errList) len() int { return len(l.list) }

// err returns every problem sorted by position, without duplicates, or nil.
func (l *errList) err() error {
	if len(l.list) == 0 {
		return nil
	}
	list := slices.Clone(l.list)
	slices.SortStableFunc(list, func(a, b *Error) int {
		return cmp.Or(cmp.Compare(a.Pos.File, b.Pos.File), cmp.Compare(a.Pos.Line, b.Pos.Line),
			cmp.Compare(a.Message, b.Message))
	})
	list = slices.CompactFunc(list, func(a, b *Error) bool { return *a == *b })
	errs := make([]error, len(list))
	for i, e := range list {
		errs[i] = e
	}
	return errors.Join(errs...)
}

// src reports problems found in one source (a file or a programmatic origin).
type src struct {
	file string
	errs *errList
}

func (s src) pos(n *yaml.Node) Pos { return Pos{File: s.file, Line: n.Line} }

func (s src) errorf(n *yaml.Node, format string, args ...any) {
	s.errs.add(s.pos(n), format, args...)
}

// mapping lists a mapping's entries in file order.
func (s src) mapping(n *yaml.Node, what string) ([]entry, bool) {
	return mappingEntries(n, func(at *yaml.Node, msg string) { s.errorf(at, "%s: %s", what, msg) })
}
