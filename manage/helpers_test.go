package manage

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
)

func newTextLog(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// ctxFake reports the actor found in the request context.
type ctxFake struct {
	*fake
	on func(audit.Actor)
}

func (c *ctxFake) Get(ctx context.Context, id ids.UUID, o content.GetOptions) (*content.View, error) {
	c.on(audit.ActorFrom(ctx))
	return c.fake.Get(ctx, id, o)
}

func testSchEmpty(t *testing.T) *schema.Schema {
	t.Helper()
	s, err := schema.NewBuilder(schema.NewRegistry()).Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
