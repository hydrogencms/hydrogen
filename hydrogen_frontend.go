package hydrogen

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/schema"
)

// redirectType is the pages module's type for manual redirects; it never renders.
const redirectType = "redirect"

// checkFrontend implements check-frontend: it compares the templates a frontend has (a JSON file
// {"templates": ["page", ...]}, one per content type name) with what the channels can serve. A page type a
// channel allows needs a template (failure). A collection mounted in the page tree needs one too (warning, and
// with -db only for real mounts; without it every other collection the channel allows is listed as a warning).
// It needs no database unless -db is given.
func checkFrontend(ctx context.Context, b *Builder, o RunOptions, args []string) error {
	fs := flag.NewFlagSet("check-frontend", flag.ContinueOnError)
	fs.SetOutput(o.Stderr)
	file := fs.String("templates", "", `JSON file {"templates": ["page", "article"]} listing the frontend's templates (required)`)
	only := fs.String("channel", "", "check one channel (default: every channel)")
	withDB := fs.Bool("db", false, "read the page tree's mounts from the database instead of warning about every collection")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" || fs.NArg() > 0 {
		return fmt.Errorf("usage: check-frontend -templates <file> [-channel name] [-db]")
	}
	have, err := readTemplates(*file)
	if err != nil {
		return err
	}
	cfg, err := config.Load(*o.Config)
	if err != nil {
		return err
	}
	sc, set, modules, err := kernel.Inspect(cfg, b.all()...)
	if err != nil {
		return err
	}
	chans := set.All()
	if *only != "" {
		ch, ok := set.Get(*only)
		if !ok {
			return fmt.Errorf("unknown channel %q (channels: %s)", *only, channelNames(chans))
		}
		chans = []*channel.Channel{ch}
	}
	var mounts map[string]map[string]bool // site -> mounted collections
	if *withDB && slices.Contains(modules, "pages") {
		app, err := newApp(ctx, b, o)
		if err != nil {
			return err
		}
		mounts, err = loadMounts(ctx, app)
		_ = app.Close()
		if err != nil {
			return err
		}
	}

	missing := 0
	for _, ch := range chans {
		pages, others := typesOf(sc, ch)
		fmt.Fprintf(o.Stdout, "channel %s\n", ch.Name)
		for _, t := range pages {
			if have[t] {
				fmt.Fprintf(o.Stdout, "  ok       page type %s\n", t)
				continue
			}
			missing++
			fmt.Fprintf(o.Stdout, "  MISSING  page type %s: no template\n", t)
		}
		var mounted map[string]bool
		if *withDB {
			mounted = mountsOf(ch, mounts)
		}
		kind := "collection"
		if *withDB {
			kind = "mounted collection"
		}
		for _, t := range others {
			switch {
			case *withDB && !mounted[t]:
			case have[t]:
				fmt.Fprintf(o.Stdout, "  ok       %s %s\n", kind, t)
			case *withDB:
				fmt.Fprintf(o.Stdout, "  warning  mounted collection %s: no template\n", t)
			default:
				fmt.Fprintf(o.Stdout, "  warning  collection %s: no template (it may be mounted in the page tree; use -db to check)\n", t)
			}
		}
	}
	for _, t := range slices.Sorted(mapKeys(have)) {
		if _, ok := sc.Type(t); !ok {
			fmt.Fprintf(o.Stdout, "warning: template %q matches no content type\n", t)
		}
	}
	if missing > 0 {
		return fmt.Errorf("check-frontend: %d page type(s) without a template", missing)
	}
	_, err = fmt.Fprintln(o.Stdout, "frontend ok")
	return err
}

func readTemplates(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("check-frontend: %w", err)
	}
	var doc struct {
		Templates []string `json:"templates"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf(`check-frontend: %s: want {"templates": ["page", ...]}: %w`, path, err)
	}
	have := make(map[string]bool, len(doc.Templates))
	for _, t := range doc.Templates {
		have[t] = true
	}
	return have, nil
}

func mapKeys(m map[string]bool) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func channelNames(chans []*channel.Channel) string {
	names := make([]string, len(chans))
	for i, c := range chans {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// typesOf splits the types a channel allows into page types and other collections (singletons and the pages
// module's redirect type are left out: nothing routes to them).
func typesOf(sc *schema.Schema, ch *channel.Channel) (pages, others []string) {
	for _, t := range sc.Types() {
		if !ch.AllowsCollection(t.Name) {
			continue
		}
		switch {
		case t.Page:
			pages = append(pages, t.Name)
		case t.Singleton, t.Name == redirectType:
		default:
			others = append(others, t.Name)
		}
	}
	slices.Sort(pages)
	slices.Sort(others)
	return pages, others
}

// loadMounts reads the pages module's mount nodes: collections mounted per site.
func loadMounts(ctx context.Context, app *kernel.App) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	rows, err := app.Services().DB.Read().Query(ctx, `SELECT DISTINCT site, collection FROM page_node WHERE kind = 'mount'`)
	if err != nil {
		return nil, fmt.Errorf("check-frontend: read mounts (run migrate first?): %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var site, coll string
		if err := rows.Scan(&site, &coll); err != nil {
			return nil, err
		}
		if out[site] == nil {
			out[site] = map[string]bool{}
		}
		out[site][coll] = true
	}
	return out, rows.Err()
}

// mountsOf returns the collections mounted in a channel's site that the channel allows. A channel that names
// no site uses the pages module's default site, which only the module knows, so every site counts then.
func mountsOf(ch *channel.Channel, mounts map[string]map[string]bool) map[string]bool {
	site, hasSite := ch.Options["site"].(string)
	hasSite = hasSite && site != ""
	out := map[string]bool{}
	for s, colls := range mounts {
		if hasSite && s != site {
			continue
		}
		for c := range colls {
			if ch.AllowsCollection(c) {
				out[c] = true
			}
		}
	}
	return out
}
