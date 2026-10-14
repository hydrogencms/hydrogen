// Package pgtest gives tests a scratch Postgres database. Import it from tests only.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// adminURL points at a Postgres where tests may create and drop databases (make db-up starts one).
func AdminURL() string {
	if u := os.Getenv("HYDROGEN_TEST_POSTGRES_URL"); u != "" {
		return u
	}
	return "postgres://hydrogen:hydrogen@localhost:5436/hydrogen?sslmode=disable"
}

// Scratch creates an empty database for one test and drops it afterwards. Without a reachable Postgres the
// test is skipped, unless HYDROGEN_TEST_REQUIRE_POSTGRES=1 (CI).
func Scratch(t testing.TB) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, AdminURL())
	if err != nil {
		if os.Getenv("HYDROGEN_TEST_REQUIRE_POSTGRES") == "1" {
			t.Fatalf("postgres unavailable: %v", err)
		}
		t.Skipf("postgres unavailable (%v); run make db-up", err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "hydrogen_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	_ = admin.Close(ctx)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if admin, err := pgx.Connect(ctx, AdminURL()); err == nil {
			_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			_ = admin.Close(ctx)
		}
	})
	u, _ := url.Parse(AdminURL())
	u.Path = "/" + name
	return u.String()
}
