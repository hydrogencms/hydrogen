package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

// probeModule exposes Local API calls that have no management route (GetForChannel, URL, LinkURL) so the tests
// can drive them over HTTP. It lives in the tests only: the /probe/* routes exist in no real build of this
// example, and main.go does not register this module.
type probeModule struct {
	svc   *kernel.Services
	pages *pages.Service
}

func (*probeModule) Name() string       { return "probe" }
func (*probeModule) Requires() []string { return []string{"pages"} }

func (p *probeModule) Register(r *kernel.Registry) error {
	r.ManageRoute(manage.Route{Method: "GET", Path: "/probe/get", Summary: "GetForChannel", Handler: p.get, Policy: manage.Authenticated})
	r.ManageRoute(manage.Route{Method: "GET", Path: "/probe/url", Summary: "URL", Handler: p.url, Policy: manage.Authenticated})
	r.ManageRoute(manage.Route{Method: "GET", Path: "/probe/link", Summary: "LinkURL", Handler: p.link, Policy: manage.Authenticated})
	return nil
}

func (p *probeModule) Boot(_ context.Context, s *kernel.Services) error {
	p.svc = s
	var ok bool
	if p.pages, ok = kernel.LookupService[*pages.Service](s); !ok {
		return errors.New("probe: pages service missing")
	}
	return nil
}

func (p *probeModule) args(w http.ResponseWriter, r *http.Request) (ids.UUID, bool) {
	id, err := ids.Parse(r.URL.Query().Get("item"))
	if err != nil {
		manage.Fail(w, r, manage.BadRequest("item must be a UUID"))
		return id, false
	}
	return id, true
}

func (p *probeModule) get(w http.ResponseWriter, r *http.Request) {
	id, ok := p.args(w, r)
	if !ok {
		return
	}
	ch, found := p.svc.Channels.Get(r.URL.Query().Get("channel"))
	if !found {
		manage.Fail(w, r, manage.BadRequest("unknown channel"))
		return
	}
	v, err := p.svc.Content.GetForChannel(r.Context(), id, ch, content.ChannelGetOptions{})
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"locale": v.Locale, "type": v.Type})
}

func (p *probeModule) url(w http.ResponseWriter, r *http.Request) {
	id, ok := p.args(w, r)
	if !ok {
		return
	}
	ch, found := p.svc.Channels.Get(r.URL.Query().Get("channel"))
	if !found {
		manage.Fail(w, r, manage.BadRequest("unknown channel"))
		return
	}
	u, has, err := p.pages.URL(r.Context(), ch, id, r.URL.Query().Get("locale"))
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"url": u, "found": has})
}

func (p *probeModule) link(w http.ResponseWriter, r *http.Request) {
	id, ok := p.args(w, r)
	if !ok {
		return
	}
	ch, found := p.svc.Channels.Get(r.URL.Query().Get("from"))
	if !found {
		manage.Fail(w, r, manage.BadRequest("unknown channel"))
		return
	}
	u, err := p.pages.LinkURL(r.Context(), ch, id)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"url": u})
}
