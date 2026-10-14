package content

import (
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

// nopDB lets unit tests build a Service; it is never used to run transactions.
type nopDB struct{ store.DB }

func nonNilOutbox() *outbox.Outbox {
	ob, err := outbox.New(nopDB{}, clock.System{}, nil, nil, outbox.Options{})
	if err != nil {
		panic(err)
	}
	return ob
}
