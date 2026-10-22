package siteexport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/features/media"
	"github.com/hydrogencms/hydrogen/modules/site"
)

// The test binary doubles as the publisher command: TestMain runs it when HELPER_MODE is set.
func TestMain(m *testing.M) {
	if os.Getenv("HELPER_MODE") == "record" {
		// args: <record file> <dir>; records the arguments and the files it sees
		var names []string
		walk := os.Getenv("HYDROGEN_EXPORT_DIR")
		if len(os.Args) > 2 {
			walk = os.Args[2]
		}
		_ = filepath.WalkDir(walk, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel, _ := filepath.Rel(walk, p)
				names = append(names, filepath.ToSlash(rel))
			}
			return nil
		})
		slices.Sort(names)
		line := strings.Join(os.Args[1:], " ") + "\nenv=" + os.Getenv("HYDROGEN_EXPORT_DIR") + "\nchanged=" + os.Getenv("HYDROGEN_EXPORT_CHANGED") + "\nfiles=" + strings.Join(names, ",") + "\n"
		if f := os.Getenv("HYDROGEN_EXPORT_CHANGED"); f != "" {
			b, _ := os.ReadFile(f)
			line += "list=" + strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", ",") + "\n"
		}
		_ = os.WriteFile(os.Args[1], []byte(line), 0o644)
		if os.Getenv("HELPER_FAIL") != "" {
			_, _ = os.Stderr.WriteString("boom")
			os.Exit(3)
		}
		return
	}
	os.Exit(m.Run())
}

func TestConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"defaults", Config{Out: "x"}, ""},
		{"no out", Config{}, "out must not be empty"},
		{"format", Config{Out: "x", Redirects: []string{"apache"}}, `unknown format "apache"`},
		{"twice", Config{Out: "x", Redirects: []string{"html", "html"}}, "listed twice"},
		{"concurrency", Config{Out: "x", Concurrency: 100}, "concurrency"},
		{"dir", Config{Out: "x", Publisher: PublisherConfig{Dir: DirConfig{Keep: 2}}}, "publisher.dir.path"},
		{"s3 keys", Config{Out: "x", Publisher: PublisherConfig{S3: S3Config{Bucket: "b"}}}, "access_key"},
		{"s3 endpoint", Config{Out: "x", Publisher: PublisherConfig{S3: S3Config{Bucket: "b", AccessKey: "a", SecretKey: "s", Endpoint: "ftp://x"}}}, "endpoint"},
		{"two", Config{Out: "x", Publisher: PublisherConfig{Dir: DirConfig{Path: "p"}, Command: CommandConfig{Argv: []string{"x"}}}}, "set one of"},
		{"command", Config{Out: "x", Publisher: PublisherConfig{Command: CommandConfig{Timeout: 1}}}, "argv"},
	} {
		err := tc.cfg.validate()
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	c := Config{Out: "x", Publisher: PublisherConfig{Dir: DirConfig{Path: "p"}}}
	if err := c.validate(); err != nil || c.Concurrency != 8 || c.Publisher.Dir.Keep != 3 {
		t.Errorf("defaults: %v %+v", err, c)
	}
}

func TestFileFor(t *testing.T) {
	en := site.ExportChannel{Name: "en", Prefix: "/en"}
	host := site.ExportChannel{Name: "web", Host: "example.com"}
	for _, tc := range []struct {
		ch          site.ExportChannel
		kind, url   string
		contentType string
		want        string
		err         bool
	}{
		{en, "page", "/en", "text/html", "en/en/index.html", false},
		{en, "page", "/en/about/team", "text/html", "en/en/about/team/index.html", false},
		{en, "not_found", "/en/__hydrogen/404", "text/html", "en/en/404.html", false},
		{host, "not_found", "/__hydrogen/404", "text/html", "example.com/404.html", false},
		{host, "page", "/", "text/html", "example.com/index.html", false},
		{host, "route", "/sitemap.xml", "application/xml", "example.com/sitemap.xml", false},
		{host, "item", "/blog/v1.2", "text/html; charset=utf-8", "example.com/blog/v1.2/index.html", false},
		{host, "route", "/tags/b%20c", "text/html", "example.com/tags/b c/index.html", false},
		{host, "route", "/feed", "application/rss+xml; charset=utf-8", "example.com/feed.xml", false},
		{host, "route", "/feed.xml", "application/rss+xml; charset=utf-8", "example.com/feed.xml", false},
		{host, "route", "/search.json", "application/json", "example.com/search.json", false},
		{en, "route", "/en/feed.xml", "application/rss+xml", "en/en/feed.xml", false},
		{host, "route", "/data", "application/json", "example.com/data.json", false},
		{host, "route", "/robots", "text/plain; charset=utf-8", "example.com/robots.txt", false},
		{host, "route", "/blob", "application/x-unknown-thing", "example.com/blob/index.html", false},
		{host, "page", "/a?page=2", "text/html", "", true},
		{host, "page", "/a/../../x", "text/html", "", true},
		{host, "page", "/a/%2e%2e/x", "text/html", "", true},
		{host, "page", "/a%5Cb", "text/html", "", true},
	} {
		got, err := fileFor(tc.ch, tc.kind, tc.url, tc.contentType)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.url, got, err, tc.want)
		}
	}
}

func TestRedirectFormats(t *testing.T) {
	rs := []redirect{
		{Root: "example.com", Host: "example.com", From: "/old", To: "/new", Status: 301},
		{Root: "example.com", Host: "example.com", From: "/a/b", To: "https://other.test/x?y=1", Status: 302},
		{Root: "example.com", Host: "example.com", From: "/q$", To: "/x", Status: 301},
		{Root: "example.com", Host: "example.com", From: "/page", To: "/z", Status: 301},
	}
	files, warns := redirectFiles([]string{"netlify", "nginx", "s3", "html"}, []string{"example.com", "empty.test"}, rs, map[string]bool{"example.com/page/index.html": true})
	by := map[string]string{}
	for _, f := range files {
		by[f.Name] = string(f.Data)
	}
	if n := by["example.com/_redirects"]; !strings.Contains(n, "/old /new 301\n") || !strings.Contains(n, "/a/b https://other.test/x?y=1 302\n") {
		t.Errorf("netlify:\n%s", n)
	}
	if n := by["redirects.nginx.conf"]; !strings.Contains(n, `"example.com/old" "/new";`) || !strings.Contains(n, `"example.com/a/b" 302;`) || strings.Contains(n, "q$") ||
		strings.Count(n, "map $host$uri $hydrogen_redirect_to") != 1 {
		t.Errorf("nginx:\n%s", n)
	}
	if len(warns) == 0 || !strings.Contains(strings.Join(warns, "\n"), "/q$") {
		t.Errorf("warnings = %v", warns)
	}
	var rules []s3Rule
	if err := json.Unmarshal([]byte(by["example.com/s3-routing-rules.json"]), &rules); err != nil || len(rules) != 4 {
		t.Fatalf("s3 rules: %v\n%s", err, by["example.com/s3-routing-rules.json"])
	}
	if rules[0].Condition.KeyPrefixEquals != "page" || rules[3].Condition.KeyPrefixEquals != "q$" {
		t.Errorf("rules are not longest prefix first: %+v", rules)
	}
	for _, r := range rules {
		if r.Condition.HTTPErrorCodeReturnedEquals != "404" {
			t.Errorf("rule %+v redirects live keys", r)
		}
	}
	ab := rules[slices.IndexFunc(rules, func(r s3Rule) bool { return r.Condition.KeyPrefixEquals == "a/b" })]
	if ab.Redirect.HostName != "other.test" || ab.Redirect.Protocol != "https" || ab.Redirect.ReplaceKeyWith != "x?y=1" || ab.Redirect.HTTPRedirectCode != "302" {
		t.Errorf("a/b rule = %+v", ab)
	}
	if h := by["example.com/old/index.html"]; !strings.Contains(h, `url=/new`) || !strings.Contains(h, `rel="canonical" href="/new"`) {
		t.Errorf("refresh page:\n%s", h)
	}
	if h := by["example.com/a/b/index.html"]; !strings.Contains(h, "https://other.test/x?y=1") {
		t.Errorf("absolute refresh page:\n%s", h)
	}
	if _, ok := by["example.com/page/index.html"]; ok {
		t.Error("a refresh page replaced a real page")
	}
	if n, ok := by["empty.test/_redirects"]; !ok || strings.Contains(n, " 301") {
		t.Errorf("empty root: %q %v", n, ok)
	}
}

func tree(t *testing.T, files map[string]string) (string, *Manifest) {
	t.Helper()
	dir := t.TempDir()
	m := newManifest()
	for f, body := range files {
		if err := writeAtomic(filepath.Join(dir, filepath.FromSlash(f)), []byte(body)); err != nil {
			t.Fatal(err)
		}
		kind := "page"
		if strings.Contains(f, "__hydrogen/assets/") {
			kind = kindAsset
		}
		m.Files[f] = Entry{Hash: hashOf([]byte(body)), Size: len(body), Kind: kind}
	}
	return dir, m
}

func readLink(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDirPublisher(t *testing.T) {
	base := filepath.Join(t.TempDir(), "public")
	clk := clock.NewManual(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC))
	p := &dirPublisher{cfg: DirConfig{Path: base, Keep: 2}, clock: clk}
	var seen []string
	for i, body := range []string{"one", "two", "three"} {
		files := map[string]string{"h/index.html": body, "h/about/index.html": "same"}
		if i == 2 {
			delete(files, "h/about/index.html")
		}
		dir, m := tree(t, files)
		clk.Advance(time.Second)
		if err := p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m}); err != nil {
			t.Fatal(err)
		}
		if got := readLink(t, filepath.Join(base, "current", "h", "index.html")); got != body {
			t.Errorf("release %d serves %q", i, got)
		}
		target, err := os.Readlink(filepath.Join(base, "current"))
		if err != nil || filepath.IsAbs(target) {
			t.Errorf("current -> %q, %v (want a relative link)", target, err)
		}
		seen = append(seen, target)
	}
	if _, err := os.Stat(filepath.Join(base, "current", "h", "about", "index.html")); err == nil {
		t.Error("a removed file is still served")
	}
	ents, _ := os.ReadDir(filepath.Join(base, "releases"))
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) != 2 || "releases/"+dirs[1] != seen[2] || "releases/"+dirs[0] != seen[1] {
		t.Errorf("kept %v, history %v", dirs, seen)
	}
	if _, err := os.Stat(filepath.Join(base, "releases", dirs[0]+".json")); err != nil {
		t.Errorf("release manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "releases", seen[0][len("releases/"):])); err == nil {
		t.Error("the oldest release was not pruned")
	}
	// An unchanged file shares its inode with the previous release.
	a, _ := os.Stat(filepath.Join(base, "releases", dirs[0], "h", "about", "index.html"))
	if a == nil {
		t.Fatal("middle release lacks about")
	}
	// A real directory where current should be is not overwritten.
	bad := filepath.Join(t.TempDir(), "pub")
	if err := os.MkdirAll(filepath.Join(bad, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir, m := tree(t, map[string]string{"x/index.html": "x"})
	if err := (&dirPublisher{cfg: DirConfig{Path: bad, Keep: 1}, clock: clk}).Publish(context.Background(), publishInput{Tree: dir, Manifest: m}); err == nil {
		t.Error("published over a real directory")
	}
}

func TestDirPublisherHardLinks(t *testing.T) {
	base := filepath.Join(t.TempDir(), "public")
	clk := clock.NewManual(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC))
	p := &dirPublisher{cfg: DirConfig{Path: base, Keep: 3}, clock: clk}
	for _, body := range []string{"one", "two"} {
		dir, m := tree(t, map[string]string{"h/index.html": body, "h/big/index.html": "same"})
		clk.Advance(time.Second)
		if err := p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m}); err != nil {
			t.Fatal(err)
		}
	}
	ents, _ := os.ReadDir(filepath.Join(base, "releases"))
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	a, _ := os.Stat(filepath.Join(base, "releases", dirs[0], "h", "big", "index.html"))
	b, _ := os.Stat(filepath.Join(base, "releases", dirs[1], "h", "big", "index.html"))
	if a == nil || b == nil || !os.SameFile(a, b) {
		t.Error("unchanged file was copied, not linked")
	}
	a, _ = os.Stat(filepath.Join(base, "releases", dirs[0], "h", "index.html"))
	b, _ = os.Stat(filepath.Join(base, "releases", dirs[1], "h", "index.html"))
	if os.SameFile(a, b) {
		t.Error("a changed file is shared between releases")
	}
}

func TestCommandPublisher(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HELPER_MODE", "record")
	dir, m := tree(t, map[string]string{"h/index.html": "x"})
	rec := filepath.Join(t.TempDir(), "rec.txt")
	p := &commandPublisher{cfg: CommandConfig{Argv: []string{exe, rec}, Timeout: time.Minute}}
	if err := p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	got := readLink(t, rec)
	if !strings.HasPrefix(got, rec+" "+dir+"\n") || !strings.Contains(got, "env="+dir) || !strings.Contains(got, "files=h/index.html") {
		t.Errorf("recorded %q", got)
	}
	// {dir} is substituted in place, and the argument list is never a shell line
	p = &commandPublisher{cfg: CommandConfig{Argv: []string{exe, rec, "{dir}; touch " + filepath.Join(dir, "pwned")}, Timeout: time.Minute}}
	_ = p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m})
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("the argument went through a shell")
	}
	// a failing command fails the publish with its output
	t.Setenv("HELPER_FAIL", "1")
	p = &commandPublisher{cfg: CommandConfig{Argv: []string{exe, rec}, Timeout: time.Minute}}
	if err := p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("failure: %v", err)
	}
}

// fakeS3 is a path-style endpoint for one bucket that verifies the request signatures.
type fakeS3 struct {
	t       *testing.T
	mu      sync.Mutex
	objects map[string]fakeObject
	log     []string
	failPut string
}

type fakeObject struct {
	data         []byte
	ctype, cache string
}

func newFakeS3(t *testing.T) (*fakeS3, *httptest.Server) {
	f := &fakeS3{t: t, objects: map[string]fakeObject{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	sum := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) {
		http.Error(w, "<Error><Code>XAmzContentSHA256Mismatch</Code></Error>", 400)
		return
	}
	check, _ := http.NewRequest(r.Method, "http://"+r.Host+r.URL.RequestURI(), nil)
	for _, h := range []string{"Content-Type", "X-Amz-Content-Sha256", "X-Amz-Security-Token"} {
		if v := r.Header.Get(h); v != "" {
			check.Header.Set(h, v)
		}
	}
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		http.Error(w, "<Error><Code>AccessDenied</Code></Error>", http.StatusForbidden)
		return
	}
	media.SignV4(check, r.Header.Get("X-Amz-Content-Sha256"), "AK", "secret/key+test", "tok", "eu-west-1", at)
	if check.Header.Get("Authorization") != r.Header.Get("Authorization") {
		http.Error(w, "<Error><Code>SignatureDoesNotMatch</Code></Error>", http.StatusForbidden)
		return
	}
	key, ok := strings.CutPrefix(r.URL.Path, "/bkt/site/")
	if !ok {
		http.Error(w, "<Error><Code>NoSuchBucket</Code></Error>", 404)
		return
	}
	f.log = append(f.log, r.Method+" "+key)
	switch r.Method {
	case "PUT":
		if f.failPut != "" && strings.Contains(key, f.failPut) {
			http.Error(w, "<Error><Code>InternalError</Code><Message>nope</Message></Error>", 500)
			return
		}
		f.objects[key] = fakeObject{body, r.Header.Get("Content-Type"), r.Header.Get("Cache-Control")}
	case "GET":
		o, ok := f.objects[key]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", 404)
			return
		}
		_, _ = w.Write(o.data)
	case "DELETE":
		delete(f.objects, key)
		w.WriteHeader(204)
	}
}

func (f *fakeS3) puts() (n int) {
	for _, l := range f.log {
		if strings.HasPrefix(l, "PUT ") && !strings.HasSuffix(l, stateKey) {
			n++
		}
	}
	return
}

func TestS3Publisher(t *testing.T) {
	fake, srv := newFakeS3(t)
	exe, _ := os.Executable()
	t.Setenv("HELPER_MODE", "record")
	rec := filepath.Join(t.TempDir(), "purge.txt")
	cfg := S3Config{Bucket: "bkt", Region: "eu-west-1", Endpoint: srv.URL, PathStyle: true, Prefix: "site",
		AccessKey: "AK", SecretKey: "secret/key+test", SessionToken: "tok", PurgeCommand: []string{exe, rec}, Timeout: time.Minute}
	p := &s3Publisher{cfg: cfg, clock: clock.NewManual(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)), conc: 4, client: srv.Client()}
	run := func(files map[string]string, full bool) error {
		dir, m := tree(t, files)
		return p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m, Full: full})
	}
	files := map[string]string{"h/index.html": "home", "h/about/index.html": "about", "h/__hydrogen/assets/site.abc.css": "css", "h/_redirects": "x", "h/tags/b c/index.html": "tag"}
	if err := run(files, false); err != nil {
		t.Fatal(err)
	}
	if got := fake.objects["h/index.html"]; string(got.data) != "home" || got.ctype != "text/html; charset=utf-8" || got.cache != "public, max-age=0, must-revalidate" {
		t.Errorf("home = %+v", got)
	}
	if got := fake.objects["h/__hydrogen/assets/site.abc.css"]; !strings.HasPrefix(got.ctype, "text/css") || !strings.Contains(got.cache, "immutable") {
		t.Errorf("asset = %+v", got)
	}
	if string(fake.objects["h/tags/b c/index.html"].data) != "tag" {
		t.Error("key with a space not uploaded")
	}
	if fake.puts() != 5 {
		t.Errorf("puts = %d, log %v", fake.puts(), fake.log)
	}
	// assets go up before pages
	firstCSS, firstHTML := slices.IndexFunc(fake.log, func(l string) bool { return strings.HasSuffix(l, ".css") }),
		slices.IndexFunc(fake.log, func(l string) bool { return strings.HasSuffix(l, "index.html") })
	if firstCSS > firstHTML {
		t.Errorf("order: %v", fake.log)
	}
	if got := readLink(t, rec); !strings.Contains(got, "list=") || !strings.Contains(got, "h/index.html") {
		t.Errorf("purge command: %q", got)
	}

	// only the changed file goes up; the removed one is deleted; the purge lists both
	fake.log = nil
	_ = os.Remove(rec)
	files["h/index.html"] = "home 2"
	delete(files, "h/about/index.html")
	if err := run(files, false); err != nil {
		t.Fatal(err)
	}
	if fake.puts() != 1 || !slices.Contains(fake.log, "DELETE h/about/index.html") || string(fake.objects["h/index.html"].data) != "home 2" {
		t.Errorf("log %v", fake.log)
	}
	if _, ok := fake.objects["h/about/index.html"]; ok {
		t.Error("removed file still in the bucket")
	}
	if got := readLink(t, rec); !strings.Contains(got, "list=h/index.html,h/about/index.html") {
		t.Errorf("purge list: %q", got)
	}

	// nothing changed: no object requests, no purge
	fake.log = nil
	_ = os.Remove(rec)
	if err := run(files, false); err != nil {
		t.Fatal(err)
	}
	if fake.puts() != 0 {
		t.Errorf("unchanged run uploaded: %v", fake.log)
	}
	if _, err := os.Stat(rec); err == nil {
		t.Error("purge ran without a change")
	}

	// full re-uploads everything
	fake.log = nil
	if err := run(files, true); err != nil || fake.puts() != 4 {
		t.Errorf("full: %v, log %v", err, fake.log)
	}

	// a failed upload fails the run and the next run retries only it
	fake.failPut = "about"
	files["h/about/index.html"] = "about again"
	if err := run(files, false); err == nil || !strings.Contains(err.Error(), "about") {
		t.Fatalf("expected the upload failure, got %v", err)
	}
	fake.failPut = ""
	fake.log = nil
	if err := run(files, false); err != nil || fake.puts() != 1 || string(fake.objects["h/about/index.html"].data) != "about again" {
		t.Errorf("retry: %v, log %v", err, fake.log)
	}

	// bad credentials fail loudly
	p.cfg.SecretKey = "wrong"
	if err := run(files, true); err == nil {
		t.Error("wrong secret accepted")
	}
}

func TestParallel(t *testing.T) {
	var mu sync.Mutex
	var cur, peak int
	seen := make([]bool, 50)
	parallel(context.Background(), 3, len(seen), func(i int) {
		mu.Lock()
		cur++
		peak = max(peak, cur)
		seen[i] = true
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		cur--
		mu.Unlock()
	})
	if peak > 3 || slices.Contains(seen, false) {
		t.Errorf("peak %d, seen %v", peak, seen)
	}
	parallel(context.Background(), 3, 0, func(int) { t.Error("ran") })
}

func TestNginxManyHosts(t *testing.T) {
	data, _ := nginx([]redirect{
		{Root: "a.test", Host: "a.test", From: "/x", To: "/y", Status: 301},
		{Root: "b.test", Host: "b.test", From: "/x", To: "/z", Status: 308},
		{Root: "en", From: "/q.html", To: "/r", Status: 302},
	})
	n := string(data)
	for _, want := range []string{`"a.test/x" "/y";`, `"b.test/x" "/z";`, `"b.test/x" 308;`, `"~^[^/]+/q\.html$" "/r";`} {
		if !strings.Contains(n, want) {
			t.Errorf("missing %s in:\n%s", want, n)
		}
	}
	if strings.Count(n, "map ") != 2 {
		t.Errorf("duplicate map variables:\n%s", n)
	}
}

func TestCheckCase(t *testing.T) {
	seen := map[string]string{}
	if err := checkCase(seen, "h/Foo/index.html"); err != nil {
		t.Fatal(err)
	}
	if err := checkCase(seen, "h/Foo/index.html"); err != nil {
		t.Error("the same file twice is not a clash")
	}
	if err := checkCase(seen, "h/foo/index.html"); err == nil {
		t.Error("case clash not found")
	}
}

func TestS3SkipsRedirectConfigAndFullDeletes(t *testing.T) {
	fake, srv := newFakeS3(t)
	cfg := S3Config{Bucket: "bkt", Region: "eu-west-1", Endpoint: srv.URL, PathStyle: true, Prefix: "site", AccessKey: "AK", SecretKey: "secret/key+test", SessionToken: "tok", Timeout: time.Minute}
	p := &s3Publisher{cfg: cfg, clock: clock.NewManual(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)), conc: 2, client: srv.Client()}
	dir, m := tree(t, map[string]string{"h/index.html": "home", "h/_redirects": "/a /b 301", "redirects.nginx.conf": "map", "h/s3-routing-rules.json": "[]"})
	for _, f := range []string{"h/_redirects", "redirects.nginx.conf", "h/s3-routing-rules.json"} {
		e := m.Files[f]
		e.Kind = kindRedirects
		m.Files[f] = e
	}
	if err := p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m}); err != nil {
		t.Fatal(err)
	}
	for k := range fake.objects {
		if k != "h/index.html" && k != stateKey {
			t.Errorf("uploaded host configuration %s", k)
		}
	}
	// an object the manifest does not know goes with -full as well
	fake.objects["h/stale/index.html"] = fakeObject{data: []byte("old")}
	state, _ := json.Marshal(map[string]string{"h/index.html": hashOf([]byte("home")), "h/stale/index.html": "x"})
	fake.objects[stateKey] = fakeObject{data: state}
	if err := p.Publish(context.Background(), publishInput{Tree: dir, Manifest: m, Full: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.objects["h/stale/index.html"]; ok {
		t.Error("-full left a removed object in the bucket")
	}
}
