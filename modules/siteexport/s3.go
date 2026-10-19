package siteexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/features/media"
)

// stateKey is the object that records what the bucket holds (file -> hash), so any machine can publish and
// only changed files are uploaded.
const stateKey = ".hydrogen-export-state.json"

type s3Publisher struct {
	cfg   S3Config
	clock clock.Clock
	conc  int
	log   *slog.Logger
	// client is set by tests.
	client *http.Client
}

func (p *s3Publisher) Publish(ctx context.Context, in publishInput) error {
	c, err := newS3Client(p.cfg, p.clock, p.client)
	if err != nil {
		return err
	}
	remote := map[string]string{}
	b, found, err := c.get(ctx, stateKey)
	if err != nil {
		return err
	}
	if found {
		if err := json.Unmarshal(b, &remote); err != nil {
			if !in.Full {
				return fmt.Errorf("s3: %s is damaged (%w): publish with -full", stateKey, err)
			}
			remote = map[string]string{}
		}
	}
	// Redirect files are host configuration, not content: they stay out of the (public) bucket.
	var files []string
	for _, f := range published(in.Manifest) {
		if k := in.Manifest.Files[f].Kind; k != kindRedirects {
			files = append(files, f)
		}
	}
	var up []string
	for _, f := range files {
		if in.Full || remote[f] != in.Manifest.Files[f].Hash {
			up = append(up, f)
		}
	}
	// Assets before pages, pages before the home page: a visitor never gets a page whose assets are missing.
	slices.SortStableFunc(up, func(a, b string) int { return uploadRank(in.Manifest, a) - uploadRank(in.Manifest, b) })
	var del []string
	for f := range remote {
		if e, ok := in.Manifest.Files[f]; !ok || e.Kind == kindRedirects {
			del = append(del, f)
		}
	}
	slices.Sort(del)

	var mu sync.Mutex
	var errs []error
	done := map[string]bool{}
	for _, group := range splitRank(in.Manifest, up) {
		parallel(context.Background(), p.conc, len(group), func(i int) {
			f := group[i]
			data, err := os.ReadFile(filepath.Join(in.Tree, filepath.FromSlash(f)))
			if err == nil {
				err = c.put(ctx, f, data, contentTypeOf(f), cacheControlOf(in.Manifest.Files[f]))
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("upload %s: %w", f, err))
				return
			}
			done[f] = true
		})
		if len(errs) > 0 {
			break
		}
	}
	state := map[string]string{}
	for _, f := range files {
		if done[f] || (!in.Full && remote[f] == in.Manifest.Files[f].Hash) {
			state[f] = in.Manifest.Files[f].Hash
		} else if h, ok := remote[f]; ok {
			state[f] = h
		}
	}
	if len(errs) == 0 {
		parallel(context.Background(), p.conc, len(del), func(i int) {
			if err := c.del(ctx, del[i]); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("delete %s: %w", del[i], err))
				state[del[i]] = remote[del[i]]
				mu.Unlock()
			}
		})
	} else {
		for _, f := range del {
			state[f] = remote[f]
		}
	}
	// Record what was done even after a failure: the next run uploads the rest.
	sb, _ := json.Marshal(state)
	if err := c.put(ctx, stateKey, sb, "application/json", "no-store"); err != nil {
		errs = append(errs, fmt.Errorf("write %s: %w", stateKey, err))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if len(p.cfg.PurgeCommand) > 0 && (len(up) > 0 || len(del) > 0) {
		f, err := os.CreateTemp("", "hydrogen-export-changed-*.txt")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(f.Name()) }()
		changed := append(slices.Clone(up), del...)
		_, werr := f.WriteString(strings.Join(changed, "\n") + "\n")
		if err := errors.Join(werr, f.Close()); err != nil {
			return err
		}
		return runArgv(ctx, p.log, p.cfg.PurgeCommand, p.cfg.Timeout, "HYDROGEN_EXPORT_DIR="+in.Tree, "HYDROGEN_EXPORT_CHANGED="+f.Name())
	}
	return nil
}

// uploadRank orders uploads: 0 assets, 1 pages, 2 redirect files and the home page, whose absence or staleness
// is the most visible.
func uploadRank(m *Manifest, f string) int {
	e := m.Files[f]
	switch {
	case e.Kind == kindAsset:
		return 0
	case e.Kind == kindRedirects || e.Kind == kindRedirect:
		return 2
	case path.Base(f) == "index.html" && strings.Count(f, "/") <= 2:
		return 2
	}
	return 1
}

func splitRank(m *Manifest, files []string) [][]string {
	var out [][]string
	for rank := 0; rank <= 2; rank++ {
		var g []string
		for _, f := range files {
			if uploadRank(m, f) == rank {
				g = append(g, f)
			}
		}
		if len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func contentTypeOf(f string) string {
	if strings.HasSuffix(f, ".html") {
		return "text/html; charset=utf-8"
	}
	if t := mime.TypeByExtension(path.Ext(f)); t != "" {
		return t
	}
	if strings.HasSuffix(f, "_redirects") || strings.HasSuffix(f, ".conf") {
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func cacheControlOf(e Entry) string {
	if e.Kind == kindAsset {
		return "public, max-age=31536000, immutable"
	}
	return "public, max-age=0, must-revalidate"
}

// ---- a small S3 client

type s3Client struct {
	cfg      S3Config
	endpoint *url.URL
	prefix   string
	region   string
	clock    clock.Clock
	http     *http.Client
}

func newS3Client(c S3Config, clk clock.Clock, hc *http.Client) (*s3Client, error) {
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://s3." + c.Region + ".amazonaws.com"
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("s3: endpoint %q must be an http(s) URL", c.Endpoint)
	}
	if hc == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = 30 * time.Second
		hc = &http.Client{Transport: tr}
	}
	if clk == nil {
		clk = clock.System{}
	}
	prefix := strings.TrimLeft(c.Prefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &s3Client{cfg: c, endpoint: u, prefix: prefix,
		region: c.Region, clock: clk, http: hc}, nil
}

func (c *s3Client) objectURL(key string) *url.URL {
	u := *c.endpoint
	base := strings.TrimRight(c.endpoint.Path, "/")
	full := c.prefix + key
	if c.cfg.PathStyle {
		u.Path = base + "/" + c.cfg.Bucket + "/" + full
	} else {
		u.Host = c.cfg.Bucket + "." + c.endpoint.Host
		u.Path = base + "/" + full
	}
	u.RawPath = media.URIEncode(u.Path) // exactly what the signature covers
	return &u
}

func (c *s3Client) do(ctx context.Context, method, key string, body []byte, hdr map[string]string, ok ...int) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.objectURL(key).String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if len(body) == 0 {
		req.Body = http.NoBody
	}
	req.ContentLength = int64(len(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	req.Header.Set("X-Amz-Content-Sha256", hash)
	media.SignV4(req, hash, c.cfg.AccessKey, c.cfg.SecretKey, c.cfg.SessionToken, c.region, c.clock.Now())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if slices.Contains(ok, resp.StatusCode) {
		return data, resp.StatusCode, nil
	}
	var x struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	_ = xml.Unmarshal(data, &x)
	return nil, resp.StatusCode, fmt.Errorf("s3: %s %s: %d %s %s", method, key, resp.StatusCode, x.Code, x.Message)
}

func (c *s3Client) put(ctx context.Context, key string, data []byte, ctype, cache string) error {
	_, _, err := c.do(ctx, http.MethodPut, key, data, map[string]string{"Content-Type": ctype, "Cache-Control": cache}, http.StatusOK)
	return err
}

func (c *s3Client) get(ctx context.Context, key string) ([]byte, bool, error) {
	b, st, err := c.do(ctx, http.MethodGet, key, nil, nil, http.StatusOK, http.StatusNotFound, http.StatusForbidden)
	if err != nil {
		return nil, false, err
	}
	if st == http.StatusForbidden { // S3 answers 403 for a missing key to a caller without s3:ListBucket
		return nil, false, nil
	}
	return b, st == http.StatusOK, nil
}

func (c *s3Client) del(ctx context.Context, key string) error {
	_, _, err := c.do(ctx, http.MethodDelete, key, nil, nil, http.StatusNoContent, http.StatusOK, http.StatusNotFound)
	return err
}
