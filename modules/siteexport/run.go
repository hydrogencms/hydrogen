package siteexport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/site"
)

// lockName is the database lock that serializes exports across processes (the command and the consumer).
const lockName = "site.export"

// runner exports the site to a tree and hands it to the publisher.
type runner struct {
	ex    *site.Exporter
	pages *pages.Service
	db    store.DB
	clock clock.Clock
	log   *slog.Logger
	cfg   *Config
	chans func(name string) (*channel.Channel, bool)
	tags  func(topic string, payload json.RawMessage) []string
	mu    sync.Mutex
}

// Options select what one run does.
type Options struct {
	// Channel limits the run to one channel (default: every rendered channel).
	Channel string
	// Out overrides the configured work tree.
	Out string
	// Full ignores the manifest: every file is rewritten, files the export does not know are removed from the
	// tree, and the publisher delivers everything again.
	Full bool
	// Tags makes the run incremental: only targets that are new or whose recorded tags intersect these are
	// rendered again. Nil renders every target (unchanged files are still not rewritten).
	Tags []string
}

// Result summarizes a run.
type Result struct {
	Out       string
	Rendered  int
	Written   []string // files created or changed
	Removed   []string
	Unchanged int
	Redirects int
	Warnings  []string
	Published bool
}

func (r *Result) String() string {
	return fmt.Sprintf("rendered %d, wrote %d, unchanged %d, removed %d, redirects %d", r.Rendered, len(r.Written), r.Unchanged, len(r.Removed), r.Redirects)
}

type job struct {
	ch   site.ExportChannel
	t    site.Target
	file string // expected file, before the content type is known
}

type outcome struct {
	job   job
	file  string
	entry Entry
	body  []byte
	redir *redirect
	err   error
	warn  string
	note  string
}

func (r *runner) run(ctx context.Context, o Options) (*Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lock, ok, err := r.db.Locker().TryLock(ctx, lockName)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("site-export: another export is running")
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()

	out := o.Out
	if out == "" {
		out = r.cfg.Out
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	old, err := LoadManifest(out)
	if err != nil {
		return nil, err
	}
	if old == nil {
		old, o.Tags = newManifest(), nil
	}
	res := &Result{Out: out}

	all := r.ex.Channels()
	var chans []site.ExportChannel
	for _, ch := range all {
		if o.Channel == "" || ch.Name == o.Channel {
			chans = append(chans, ch)
		}
	}
	if len(chans) == 0 {
		if o.Channel != "" {
			return nil, fmt.Errorf("site-export: channel %q does not render (render: go)", o.Channel)
		}
		return nil, errors.New("site-export: no channel sets render: go")
	}
	if o.Tags == nil || slices.Contains(o.Tags, cachetag.All) {
		r.ex.Purge(cachetag.All)
		o.Tags = nil
	} else {
		r.ex.Purge(o.Tags...)
	}

	// Plan: which files each channel has now, and which of them need rendering.
	var jobs []job
	planned := map[string]bool{} // files the export keeps without rendering them again
	owner := map[string]string{}
	pageKeys := map[string]bool{}
	lower := map[string]string{}
	for _, ch := range chans {
		targets, err := r.ex.Targets(ctx, ch.Name)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			file, err := fileFor(ch, t.Kind, t.URL, "text/html")
			if err != nil {
				return nil, err
			}
			if prev, dup := owner[file]; dup && prev != t.URL {
				return nil, fmt.Errorf("site-export: %s and %s both map to %s", prev, t.URL, file)
			}
			owner[file] = t.URL
			pageKeys[rootDir(ch)+" "+t.URL] = true
			if err := checkCase(lower, file); err != nil {
				return nil, err
			}
			e, known := old.Files[file]
			stale := o.Tags == nil || o.Full || !known || e.URL != t.URL || e.Channel != ch.Name || intersects(e.Tags, o.Tags)
			if stale {
				jobs = append(jobs, job{ch: ch, t: t, file: file})
			} else {
				planned[file] = true
			}
		}
	}

	next := newManifest()
	next.Generated = r.clock.Now().UTC().Format(time.RFC3339)
	for f := range planned {
		next.Files[f] = old.Files[f]
	}

	outcomes := make([]outcome, len(jobs))
	parallel(ctx, r.cfg.Concurrency, len(jobs), func(i int) { outcomes[i] = r.render(ctx, jobs[i]) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res.Rendered = len(jobs)

	var failures []error
	var redirs []redirect
	for _, oc := range outcomes {
		switch {
		case oc.err != nil:
			failures = append(failures, oc.err)
			if e, ok := old.Files[oc.job.file]; ok { // keep what is on disk; it renders again on retry
				next.Files[oc.job.file] = e
				planned[oc.job.file] = true
			}
		case oc.warn != "":
			res.Warnings = append(res.Warnings, oc.warn)
		case oc.redir == nil && oc.note != "":
			res.Warnings = append(res.Warnings, oc.note)
			next.Files[oc.file] = oc.entry
		case oc.redir != nil:
			redirs = append(redirs, *oc.redir)
		default:
			next.Files[oc.file] = oc.entry
		}
	}

	// Write the rendered files.
	var changed, removed []string
	write := func(file string, body []byte, e Entry) error {
		if prev, ok := old.Files[file]; ok && prev.Hash == e.Hash && !o.Full {
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(file))); err == nil {
				res.Unchanged++
				return nil
			}
		}
		if err := writeAtomic(filepath.Join(out, filepath.FromSlash(file)), body); err != nil {
			return err
		}
		changed = append(changed, file)
		return nil
	}
	var wmu sync.Mutex
	var werrs []error
	parallel(ctx, r.cfg.Concurrency, len(outcomes), func(i int) {
		oc := outcomes[i]
		if oc.err != nil || oc.warn != "" || oc.redir != nil {
			return
		}
		wmu.Lock()
		defer wmu.Unlock()
		if err := write(oc.file, oc.body, oc.entry); err != nil {
			werrs = append(werrs, err)
			delete(next.Files, oc.file)
		}
	})
	res.Unchanged += len(planned)
	failures = append(failures, werrs...)

	// Assets of every selected root.
	assets, err := r.ex.Assets()
	if err != nil {
		return nil, err
	}
	roots := map[string]bool{}
	for _, ch := range chans {
		roots[rootDir(ch)] = true
	}
	allRoots := map[string]bool{}
	for _, ch := range all {
		allRoots[rootDir(ch)] = true
	}
	for _, root := range slices.Sorted(mapKeys(roots)) {
		for _, a := range assets {
			file, err := assetFile(root, a.URL)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			e := Entry{Hash: hashOf(a.Data), Size: len(a.Data), Kind: kindAsset, Root: root, URL: a.URL}
			if err := write(file, a.Data, e); err != nil {
				failures = append(failures, err)
				continue
			}
			next.Files[file] = e
		}
	}

	if len(failures) > 0 {
		// Keep the manifest true to the disk, but neither delete, add redirects nor publish a partial site.
		for f, e := range old.Files {
			if _, ok := next.Files[f]; !ok {
				next.Files[f] = e
			}
		}
		if err := next.save(out); err != nil {
			failures = append(failures, err)
		}
		return res, fmt.Errorf("site-export: %d failures, first: %w", len(failures), errors.Join(failures[:min(len(failures), 5)]...))
	}

	// Files of the selected scope that the export no longer produces: gone routes.
	var gone []Entry
	for f, e := range old.Files {
		if _, ok := next.Files[f]; ok {
			continue
		}
		inScope := false
		switch e.Kind {
		case kindRedirect, kindRedirects:
			inScope = true
		case kindAsset:
			inScope = roots[e.Root]
		default:
			inScope = e.Channel == "" || slices.ContainsFunc(chans, func(c site.ExportChannel) bool { return c.Name == e.Channel })
		}
		if !inScope {
			next.Files[f] = e
			continue
		}
		removed = append(removed, f)
		if e.URL != "" && e.Channel != "" && e.Kind != site.TargetNotFound && e.Kind != kindAsset {
			gone = append(gone, e)
		}
	}
	if o.Full {
		extra, err := untracked(out, next.Files)
		if err != nil {
			return nil, err
		}
		removed = append(removed, extra...)
	}

	// Redirects: the route table, targets that moved, and gone routes that now redirect.
	rs, warns, err := r.redirects(ctx, all, gone)
	if err != nil {
		return nil, err
	}
	res.Warnings = append(res.Warnings, warns...)
	rs = append(rs, redirs...)
	taken := map[string]bool{}
	for f, e := range next.Files {
		if e.Kind != kindRedirect && e.Kind != kindRedirects {
			taken[f], taken[strings.ToLower(f)] = true, true
		}
	}
	rs = dedupe(rs, pageKeys)
	res.Redirects = len(rs)
	files, warns := redirectFiles(r.cfg.Redirects, slices.Sorted(mapKeys(allRoots)), rs, taken)
	res.Warnings = append(res.Warnings, warns...)
	for _, rf := range files {
		e := Entry{Hash: hashOf(rf.Data), Size: len(rf.Data), Kind: rf.Kind, Root: rf.Root}
		if err := write(rf.Name, rf.Data, e); err != nil {
			return nil, err
		}
		next.Files[rf.Name] = e
	}
	// Redirect files that are written again stay; those of lifted redirects or unconfigured formats go.
	removed = slices.DeleteFunc(removed, func(f string) bool { _, ok := next.Files[f]; return ok })

	slices.Sort(removed)
	removed = slices.Compact(removed)
	for _, f := range removed {
		if err := os.Remove(filepath.Join(out, filepath.FromSlash(f))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		pruneEmpty(out, filepath.Dir(filepath.Join(out, filepath.FromSlash(f))))
	}
	slices.Sort(changed)
	res.Written, res.Removed = changed, removed
	if err := next.save(out); err != nil {
		return nil, err
	}

	if pub := r.publisher(); pub != nil {
		// Publish whenever the tree differs from what was last published, not only when this run changed it:
		// a failed publish (or a failed render before it) is retried by the next run.
		state := publishedState{Publisher: r.publisherID(), Digest: next.digest()}
		if prev := loadPublished(out); o.Full || prev != state {
			if err := pub.Publish(ctx, publishInput{Tree: out, Manifest: next, Changed: changed, Removed: removed, Full: o.Full}); err != nil {
				return res, fmt.Errorf("site-export: publish: %w", err)
			}
			if err := savePublished(out, state); err != nil {
				return res, err
			}
			res.Published = true
		}
	}
	return res, nil
}

func (r *runner) render(ctx context.Context, j job) outcome {
	oc := outcome{job: j}
	rd, err := r.ex.Render(ctx, j.ch.Name, j.t.URL)
	if err != nil {
		oc.err = fmt.Errorf("%s: %w", j.t.URL, err)
		return oc
	}
	switch {
	case rd.Status >= 300 && rd.Status < 400 && rd.Location() != "":
		oc.redir = &redirect{Root: rootDir(j.ch), Host: j.ch.Host, From: j.t.URL, To: rd.Location(), Status: rd.Status}
		return oc
	case rd.Status == http.StatusOK, rd.Status == http.StatusNotFound && j.t.Kind == site.TargetNotFound:
	default:
		oc.warn = fmt.Sprintf("%s answered %d and was not written", j.t.URL, rd.Status)
		return oc
	}
	file, err := fileFor(j.ch, j.t.Kind, j.t.URL, rd.Header.Get("Content-Type"))
	if err != nil {
		oc.err = err
		return oc
	}
	if ct := rd.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") && !strings.HasSuffix(file, "/index.html") && !strings.HasSuffix(file, "/404.html") &&
		!strings.HasSuffix(strings.SplitN(j.t.URL, "?", 2)[0], path.Ext(file)) {
		oc.note = fmt.Sprintf("%s answers %s and was written to %s: serve it at %s with a rewrite", j.t.URL, ct, file, j.t.URL)
	}
	oc.file, oc.body = file, rd.Body
	oc.entry = Entry{Hash: hashOf(rd.Body), Size: len(rd.Body), Kind: j.t.Kind, Channel: j.ch.Name, URL: j.t.URL, Tags: rd.Tags}
	return oc
}

// redirects lists the redirects of every channel from the pages module's route table (former paths of moved
// items and manual redirects), as a visitor of the live site would get them, plus the paths of gone files that
// answer a redirect now.
func (r *runner) redirects(ctx context.Context, chans []site.ExportChannel, gone []Entry) ([]redirect, []string, error) {
	rows, err := r.pages.Redirects(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("site-export: read the route table: %w", err)
	}
	type cand struct {
		ch  site.ExportChannel
		url string
	}
	var cands []cand
	for _, ec := range chans {
		ch, ok := r.chans(ec.Name)
		if !ok {
			continue
		}
		siteName, _ := ch.Options["site"].(string)
		for _, x := range rows {
			if x.Site != siteName || (x.Locale != "" && !slices.Contains(ch.Languages, x.Locale)) {
				continue
			}
			rel, _ := r.pages.SiteURL(ch, x.Path)
			cands = append(cands, cand{ec, rel})
		}
	}
	for _, g := range gone {
		for _, ec := range chans {
			if ec.Name == g.Channel {
				cands = append(cands, cand{ec, g.URL})
			}
		}
	}
	found := make([]*redirect, len(cands))
	var errs []error
	var mu sync.Mutex
	parallel(ctx, r.cfg.Concurrency, len(cands), func(i int) {
		c := cands[i]
		rd, err := r.ex.Render(ctx, c.ch.Name, c.url)
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
			return
		}
		if rd.Status >= 300 && rd.Status < 400 && rd.Location() != "" {
			found[i] = &redirect{Root: rootDir(c.ch), Host: c.ch.Host, From: c.url, To: rd.Location(), Status: rd.Status}
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	var out []redirect
	for _, f := range found {
		if f != nil {
			out = append(out, *f)
		}
	}
	var warns []string
	for _, err := range errs {
		warns = append(warns, "redirect check: "+err.Error())
	}
	return out, warns, nil
}

// dedupe drops repeated sources and sources that are pages of the same root (a page wins over a redirect).
func dedupe(rs []redirect, pages map[string]bool) []redirect {
	seen := map[string]bool{}
	var out []redirect
	for _, r := range rs {
		k := r.Root + " " + r.From
		if seen[k] || pages[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b redirect) int { return strings.Compare(a.Root+" "+a.From, b.Root+" "+b.From) })
	return out
}

func intersects(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func mapKeys[K comparable, V any](m map[K]V) func(yield func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// parallel runs fn(0..n-1) on at most workers goroutines and waits.
func parallel(ctx context.Context, workers, n int, fn func(i int)) {
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	idx := make(chan int)
	for range min(workers, max(n, 1)) {
		wg.Go(func() {
			for i := range idx {
				if ctx.Err() == nil {
					fn(i)
				}
			}
		})
	}
	for i := range n {
		idx <- i
	}
	close(idx)
	wg.Wait()
}

// untracked lists the files of the tree (other than the manifest) the export does not know.
func untracked(dir string, known map[string]Entry) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if _, ok := known[rel]; !ok && rel != ManifestName {
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

// pruneEmpty removes empty directories from dir upwards, stopping at root.
func pruneEmpty(root, dir string) {
	for dir != root && strings.HasPrefix(dir, root) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// checkCase fails when two files differ only in case: a case-insensitive host or file system would serve one
// of them for both.
func checkCase(seen map[string]string, file string) error {
	k := strings.ToLower(file)
	if prev, dup := seen[k]; dup && prev != file {
		return fmt.Errorf("site-export: %s and %s differ only in case: a case-insensitive host or file system would serve one for both", prev, file)
	}
	seen[k] = file
	return nil
}
