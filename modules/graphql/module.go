// Package graphql serves the delivery API as GraphQL: GET and POST /api/deliver/v1/graphql, with a schema
// generated per channel from the compiled content schema. Resolvers call the delivery API in process, so
// availability, includes, assets, links, previews, access and cache tags are the REST API's. Read-only.
package graphql

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

const (
	endpoint    = "/graphql"
	sdlEndpoint = "/graphql/schema.graphql"
)

// Module returns the GraphQL delivery module.
func Module() kernel.Module { return &module{cfg: defaultConfig()} }

type module struct {
	cfg Config
	svc *kernel.Services
	// observe is called with the number of delivery calls a request made (tests).
	observe func(calls int)

	mu      sync.Mutex
	schemas map[string]*chanSchema
}

func (*module) Name() string  { return "graphql" }
func (m *module) Config() any { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	if err := m.cfg.validate(); err != nil {
		return err
	}
	r.DeliveryRoute(delivery.Route{Method: http.MethodGet, Post: true, Path: endpoint, Handler: m.serve,
		Summary: "Query the channel's content with GraphQL", OpenAPI: openAPIOperation()})
	r.DeliveryRoute(delivery.Route{Method: http.MethodGet, Path: sdlEndpoint, Handler: m.serveSDL,
		Summary: "The channel's GraphQL schema as SDL"})
	return nil
}

func (m *module) Boot(_ context.Context, s *kernel.Services) error {
	m.svc = s
	m.schemas = map[string]*chanSchema{}
	return nil
}

// Start builds every channel's schema, so a schema that cannot be built stops the server instead of the first
// request.
func (m *module) Start(context.Context) error {
	for _, ch := range m.svc.Channels.All() {
		if _, err := m.schemaFor(ch); err != nil {
			return err
		}
	}
	return nil
}

// schemaFor returns the channel's schema, building it on first use.
func (m *module) schemaFor(ch *channel.Channel) (*chanSchema, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cs := m.schemas[ch.Name]; cs != nil {
		return cs, nil
	}
	caps := m.probe()
	cs, err := buildSchema(m.svc.Schema, ch, caps)
	if err != nil {
		return nil, fmt.Errorf("graphql: channel %s: %w", ch.Name, err)
	}
	m.schemas[ch.Name] = cs
	return cs, nil
}
