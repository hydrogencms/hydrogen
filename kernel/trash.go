package kernel

import (
	"context"
	"time"
)

// DefaultTrashRetention is how long trashed items stay in the trash by default.
const DefaultTrashRetention = 720 * time.Hour

// purgeTrash is the content.trash job: it purges items that have been in the trash longer than retention, as
// the system. Zero retention keeps them.
func purgeTrash(retention time.Duration) func(context.Context, *Services) error {
	return func(ctx context.Context, s *Services) error {
		if retention <= 0 {
			return nil
		}
		n, err := s.Content.PurgeExpired(ctx, s.Clock.Now().Add(-retention))
		if n > 0 {
			s.Log.Info("expired trash purged", "count", n)
		}
		return err
	}
}
