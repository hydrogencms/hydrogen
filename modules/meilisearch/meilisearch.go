// Package meilisearch is the Meilisearch search module: it keeps one Meilisearch index in step with the
// published content and answers the search API from it, in place of the database's full-text search. It talks
// to the server over HTTP with the standard library only. See docs/meilisearch.md.
package meilisearch

import (
	"net/http"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

// Module returns the Meilisearch module.
func Module() kernel.Module { return &module{cfg: defaults()} }

type module struct {
	cfg Config
}

func (*module) Name() string  { return "meilisearch" }
func (m *module) Config() any { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	if err := m.cfg.validate(r.Env() == config.DevEnv); err != nil {
		return err
	}
	return r.SearchEngine(New(m.cfg, &http.Client{}))
}
