package storeutil

import "github.com/hydrogencms/hydrogen/core/store"

// NormalizePage applies the paging defaults and bounds of admin lists.
func NormalizePage(p store.Page) store.Page {
	switch {
	case p.Limit <= 0:
		p.Limit = DefaultLimit
	case p.Limit > MaxLimit:
		p.Limit = MaxLimit
	}
	p.Offset = max(p.Offset, 0)
	return p
}
