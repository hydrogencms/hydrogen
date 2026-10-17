package kernel

import (
	"context"

	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/features/collab"
)

// CollabConfig configures co-editing of items in the admin.
type CollabConfig struct {
	// Enabled lets editors of the same item see each other and edit together (default true). Off, the routes
	// are not served and the admin edits as before.
	Enabled bool `yaml:"enabled"`
}

// wireCollab builds the collaboration service.
func (s *Services) wireCollab() error {
	if !s.Core.Collab.Enabled {
		return nil
	}
	c, err := collab.New(collab.Options{DB: s.DB, Content: s.Content, Clock: s.Clock, IDs: s.IDs, Log: s.Log})
	s.Collab = c
	return err
}

// collabConsumer is the outbox consumer of collaboration; the service it calls is built after the outbox.
func collabConsumer(svc func() *collab.Service) outbox.Consumer {
	return outbox.Consumer{
		Name:   collab.ConsumerName,
		Topics: collab.Topics,
		Handle: func(ctx context.Context, e outbox.Event) error { return svc().Consumer().Handle(ctx, e) },
	}
}
