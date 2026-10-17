package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storetest"
)

func open(t *testing.T) store.DB {
	db, err := Open(context.Background(), "sqlite:"+filepath.Join(t.TempDir(), "test.db"), store.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestConformance(t *testing.T) { storetest.Run(t, open) }

func TestURL(t *testing.T) {
	for _, u := range []string{"postgres://x", "sqlite:", "sqlite:a.db?mode=ro"} {
		if _, err := Open(context.Background(), u, store.OpenOptions{}); err == nil {
			t.Errorf("Open(%q) succeeded", u)
		}
	}
}
