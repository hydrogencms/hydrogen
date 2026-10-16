package tests

import (
	"testing"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
)

// hardDelete moves an item to the trash and purges it: what Delete did before the trash existed.
func hardDelete(t testing.TB, svc *content.Service, id ids.UUID) {
	t.Helper()
	if err := svc.Trash(ctx, id); err != nil {
		t.Fatalf("trash %s: %v", id, err)
	}
	if err := svc.Purge(ctx, id); err != nil {
		t.Fatalf("purge %s: %v", id, err)
	}
}

func trashedIDs(t testing.TB, svc *content.Service, typ string) []ids.UUID {
	t.Helper()
	res, err := svc.ListTrash(ctx, content.TrashOptions{Type: typ})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]ids.UUID, len(res.Items))
	for i, it := range res.Items {
		out[i] = it.ID
	}
	return out
}
