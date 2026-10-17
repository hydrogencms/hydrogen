package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
)

// fakeS3 is a path-style S3 endpoint for one bucket that verifies request signatures and payload hashes.
type fakeS3 struct {
	t       *testing.T
	bucket  string
	creds   credentials
	region  string
	mu      sync.Mutex
	objects map[string]fakeObject
	// ignoreRange makes GET answer 200 with the whole object.
	ignoreRange bool
	// hideMissing answers 403 instead of 404 for a missing key, as S3 does for a caller without s3:ListBucket.
	hideMissing bool
	requests    []string
}

type fakeObject struct {
	data  []byte
	ctype string
	cache string
	disp  string
	mod   time.Time
}

func newFakeS3(t *testing.T) (*fakeS3, *httptest.Server) {
	f := &fakeS3{t: t, bucket: "bkt", region: "eu-west-1", objects: map[string]fakeObject{},
		creds: credentials{AccessKey: "AKIATEST", SecretKey: "secret/key+test", SessionToken: "tok"}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeS3) fail(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>`+code+`</Code><Message>`+msg+`</Message></Error>`)
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
	body, _ := io.ReadAll(r.Body)

	// The signature must be the one the client would compute for the request as received.
	check, _ := http.NewRequest(r.Method, "http://"+r.Host+r.URL.RequestURI(), nil)
	for _, h := range []string{"Range", "Content-Type", "Date", "Content-Md5", "X-Amz-Content-Sha256", "X-Amz-Security-Token"} {
		if v := r.Header.Get(h); v != "" {
			check.Header.Set(h, v)
		}
	}
	at, err := time.Parse(amzDateFormat, r.Header.Get("X-Amz-Date"))
	if err != nil {
		f.fail(w, 403, "AccessDenied", "no date")
		return
	}
	signV4(check, r.Header.Get("X-Amz-Content-Sha256"), f.creds, f.region, "s3", at)
	if check.Header.Get("Authorization") != r.Header.Get("Authorization") {
		f.fail(w, 403, "SignatureDoesNotMatch", "bad signature")
		return
	}
	if r.Header.Get("X-Amz-Security-Token") != "tok" {
		f.fail(w, 403, "InvalidToken", "no session token")
		return
	}
	if h := r.Header.Get("X-Amz-Content-Sha256"); h != unsignedBody {
		sum := sha256.Sum256(body)
		if h != hex.EncodeToString(sum[:]) {
			f.fail(w, 400, "XAmzContentSHA256Mismatch", "payload hash")
			return
		}
	}

	rest, ok := strings.CutPrefix(r.URL.Path, "/"+f.bucket)
	if !ok {
		f.fail(w, 404, "NoSuchBucket", "no such bucket")
		return
	}
	key := strings.TrimPrefix(rest, "/")
	switch {
	case r.Method == http.MethodGet && key == "":
		f.list(w, r)
	case r.Method == http.MethodPut:
		if r.ContentLength != int64(len(body)) {
			f.fail(w, 400, "IncompleteBody", "short body")
			return
		}
		f.objects[key] = fakeObject{data: body, ctype: r.Header.Get("Content-Type"), cache: r.Header.Get("Cache-Control"), disp: r.Header.Get("Content-Disposition"), mod: time.Now().UTC().Truncate(time.Second)}
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		o, ok := f.objects[key]
		if !ok {
			if f.hideMissing {
				f.fail(w, 403, "AccessDenied", "Access Denied")
				return
			}
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			f.fail(w, 404, "NoSuchKey", "no such key")
			return
		}
		w.Header().Set("Content-Type", o.ctype)
		if f.ignoreRange {
			r.Header.Del("Range")
		}
		http.ServeContent(w, r, "", o.mod, bytes.NewReader(o.data))
	default:
		f.fail(w, 405, "MethodNotAllowed", "method")
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("list-type") != "2" {
		f.fail(w, 400, "InvalidArgument", "list-type")
		return
	}
	max, _ := strconv.Atoi(q.Get("max-keys"))
	if max <= 0 || max > 3 { // small pages exercise continuation
		max = 3
	}
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, q.Get("prefix")) && k > q.Get("start-after") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	truncated := len(keys) > max
	if truncated {
		keys = keys[:max]
	}
	type content struct {
		Key          string
		Size         int
		LastModified string
	}
	var res struct {
		XMLName     xml.Name `xml:"ListBucketResult"`
		IsTruncated bool
		Contents    []content
	}
	res.IsTruncated = truncated
	for _, k := range keys {
		res.Contents = append(res.Contents, content{k, len(f.objects[k].data), f.objects[k].mod.Format(time.RFC3339)})
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(res)
}

func s3For(t *testing.T, f *fakeS3, srv *httptest.Server, prefix string) *S3 {
	s, err := NewS3(S3Config{Bucket: f.bucket, Region: f.region, Endpoint: srv.URL, PathStyle: true, Prefix: prefix,
		AccessKey: f.creds.AccessKey, SecretKey: f.creds.SecretKey, SessionToken: f.creds.SessionToken,
		Clock: clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestS3Storage(t *testing.T) {
	f, srv := newFakeS3(t)
	testStorage(t, s3For(t, f, srv, ""))
}

func TestS3StorageWithPrefix(t *testing.T) {
	f, srv := newFakeS3(t)
	s := s3For(t, f, srv, "media/")
	testStorage(t, s)
	other := fakeObject{data: []byte("x")}
	f.mu.Lock()
	f.objects["other/"+keyOf("o")] = other
	f.mu.Unlock()
	got, err := s.List(context.Background(), "", "", 100)
	if err != nil || slices.Contains(entryKeys(got), keyOf("o")) || slices.ContainsFunc(entryKeys(got), func(k string) bool { return strings.Contains(k, "/") }) {
		t.Fatalf("list sees objects outside the prefix: %v %v", got, err)
	}
}

func TestS3PutDetails(t *testing.T) {
	f, srv := newFakeS3(t)
	s := s3For(t, f, srv, "p")
	ctx := context.Background()
	key := keyOf("d")

	// A seekable body is hashed and signed; headers carry the type and caching.
	if err := s.Put(ctx, key, bytes.NewReader([]byte("seekable")), 8, "image/png"); err != nil {
		t.Fatal(err)
	}
	o := f.objects["p/"+key]
	if string(o.data) != "seekable" || o.ctype != "image/png" || !strings.Contains(o.cache, "immutable") {
		t.Fatalf("stored object: %+v", o)
	}
	// A plain reader over http is buffered and hashed too (unsigned payloads are for TLS only).
	if err := s.Put(ctx, keyOf("e"), io.MultiReader(strings.NewReader("plain")), 5, ""); err != nil {
		t.Fatal(err)
	}
	if o := f.objects["p/"+keyOf("e")]; string(o.data) != "plain" || o.ctype != "application/octet-stream" {
		t.Fatalf("stored object: %+v", o)
	}
}

// Without s3:ListBucket a missing key answers 403: Stat reports it as missing, so uploads store it and garbage
// collection (which needs the listing anyway) is the only feature that needs the permission.
func TestS3StatTreatsForbiddenAsMissing(t *testing.T) {
	f, srv := newFakeS3(t)
	f.hideMissing = true
	s := s3For(t, f, srv, "")
	ctx := context.Background()
	if _, err := s.Stat(ctx, keyOf("m")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat of a missing key under 403: %v", err)
	}
	if err := s.Put(ctx, keyOf("m"), strings.NewReader("data"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Stat(ctx, keyOf("m")); err != nil || info.Size != 4 {
		t.Fatalf("stat: %+v %v", info, err)
	}
	// Only HEAD is read that way: a GET of a missing key under 403 stays an error.
	if _, _, err := s.Get(ctx, keyOf("n")); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("get of a missing key under 403: %v", err)
	}
}

func TestS3PutSetsServingHeaders(t *testing.T) {
	f, srv := newFakeS3(t)
	s := s3For(t, f, srv, "")
	ctx := context.Background()
	for _, tc := range []struct{ mime, ctype, disp string }{
		{"image/png", "image/png", "inline"},
		{"application/pdf", "application/pdf", "inline"},
		{"image/svg+xml", "image/svg+xml", "attachment"},
		{"text/html", "text/html; charset=utf-8", "attachment"},
		{"text/csv", "text/csv; charset=utf-8", "attachment"},
		{"application/zip", "application/zip", "attachment"},
		{"", "application/octet-stream", "attachment"},
	} {
		key := keyOf("h" + tc.mime)
		if err := s.Put(ctx, key, strings.NewReader("data"), 4, tc.mime); err != nil {
			t.Fatal(err)
		}
		o := f.objects[key]
		if o.ctype != tc.ctype || o.disp != tc.disp || o.cache != "public, max-age=31536000, immutable" {
			t.Errorf("%q: stored %+v", tc.mime, o)
		}
	}
}

func TestS3RangeIgnoredByService(t *testing.T) {
	f, srv := newFakeS3(t)
	f.ignoreRange = true
	s := s3For(t, f, srv, "")
	key := keyOf("r")
	if err := s.Put(context.Background(), key, strings.NewReader("0123456789"), 10, ""); err != nil {
		t.Fatal(err)
	}
	rc, info, err := s.GetRange(context.Background(), key, 3, 4)
	if err != nil || info.Size != 10 || readAll(t, rc) != "3456" {
		t.Fatalf("range against a service that ignores it: %+v %v", info, err)
	}
}

func TestS3Errors(t *testing.T) {
	f, srv := newFakeS3(t)
	bad, err := NewS3(S3Config{Bucket: f.bucket, Endpoint: srv.URL, PathStyle: true, AccessKey: "AKIATEST", SecretKey: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	err = bad.Put(context.Background(), keyOf("a"), strings.NewReader("x"), 1, "")
	var se *S3Error
	if !errors.As(err, &se) || se.Status != 403 || se.Code != "SignatureDoesNotMatch" {
		t.Fatalf("bad credentials: %v", err)
	}
	wrongBucket, _ := NewS3(S3Config{Bucket: "nope", Region: f.region, Endpoint: srv.URL, PathStyle: true,
		AccessKey: f.creds.AccessKey, SecretKey: f.creds.SecretKey, SessionToken: "tok"})
	if _, err := wrongBucket.List(context.Background(), "", "", 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("missing bucket: %v", err)
	}
}

func TestS3Config(t *testing.T) {
	for name, c := range map[string]S3Config{
		"no bucket": {AccessKey: "a", SecretKey: "b"},
		"no keys":   {Bucket: "b"},
		"bad url":   {Bucket: "b", AccessKey: "a", SecretKey: "b", Endpoint: "ftp://x"},
	} {
		if _, err := NewS3(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	virtual, _ := NewS3(S3Config{Bucket: "b", Region: "eu-west-1", AccessKey: "a", SecretKey: "b", Prefix: "/m"})
	if got := virtual.objectURL(virtual.prefix + "abcd").String(); got != "https://b.s3.eu-west-1.amazonaws.com/m/abcd" {
		t.Errorf("virtual-hosted url = %s", got)
	}
	path, _ := NewS3(S3Config{Bucket: "b", Endpoint: "http://minio:9000/", PathStyle: true, AccessKey: "a", SecretKey: "b"})
	if got := path.objectURL("abcd").String(); got != "http://minio:9000/b/abcd" {
		t.Errorf("path-style url = %s", got)
	}
}

// TestRealS3 runs the storage contract against a real S3-compatible endpoint when HYDROGEN_TEST_S3_* is set:
// ENDPOINT (optional), BUCKET, REGION (optional), ACCESS_KEY, SECRET_KEY, PATH_STYLE=1 for MinIO. Objects are
// created under a random prefix and removed afterwards.
func TestRealS3(t *testing.T) {
	bucket := os.Getenv("HYDROGEN_TEST_S3_BUCKET")
	if bucket == "" {
		t.Skip("HYDROGEN_TEST_S3_BUCKET not set")
	}
	prefix := "hydrogen-test-" + keyOf(time.Now().String())[:12] + "/"
	s, err := NewS3(S3Config{Bucket: bucket, Region: os.Getenv("HYDROGEN_TEST_S3_REGION"), Endpoint: os.Getenv("HYDROGEN_TEST_S3_ENDPOINT"),
		PathStyle: os.Getenv("HYDROGEN_TEST_S3_PATH_STYLE") == "1", Prefix: prefix,
		AccessKey: os.Getenv("HYDROGEN_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("HYDROGEN_TEST_S3_SECRET_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for {
			es, err := s.List(ctx, "", "", 100)
			if err != nil || len(es) == 0 {
				return
			}
			for _, e := range es {
				_ = s.Delete(ctx, e.Key)
			}
		}
	})
	testStorage(t, s)
}
