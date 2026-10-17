package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
)

// S3Config configures the S3-compatible driver (AWS S3, MinIO, Cloudflare R2, ...).
type S3Config struct {
	Bucket string
	Region string // default "us-east-1"
	// Endpoint is the service URL, e.g. "http://localhost:9000"; default https://s3.<region>.amazonaws.com.
	Endpoint string
	// PathStyle addresses the bucket in the path (endpoint/bucket/key) instead of the host
	// (bucket.endpoint/key). MinIO and most self-hosted services need it.
	PathStyle bool
	// Prefix is prepended to every key ("media/").
	Prefix       string
	AccessKey    string
	SecretKey    string
	SessionToken string
	Client       *http.Client // default: a client with a response header timeout
	Clock        clock.Clock  // default clock.System
}

// S3 stores files in an S3-compatible bucket with single-request PUTs, signing requests itself.
type S3 struct {
	c        S3Config
	endpoint *url.URL
	prefix   string
	creds    credentials
}

// NewS3 validates the configuration; it makes no request.
func NewS3(c S3Config) (*S3, error) {
	if c.Bucket == "" {
		return nil, errors.New("media: s3 storage needs a bucket")
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return nil, errors.New("media: s3 storage needs access_key and secret_key")
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://s3." + c.Region + ".amazonaws.com"
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("media: s3 endpoint %q must be an http(s) URL", c.Endpoint)
	}
	if c.Client == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = 30 * time.Second
		c.Client = &http.Client{Transport: tr}
	}
	if c.Clock == nil {
		c.Clock = clock.System{}
	}
	prefix := strings.TrimLeft(c.Prefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &S3{c: c, endpoint: u, prefix: prefix, creds: credentials{c.AccessKey, c.SecretKey, c.SessionToken}}, nil
}

// objectURL addresses an object, or the bucket itself for an empty key.
func (s *S3) objectURL(key string) *url.URL {
	u := *s.endpoint
	base := strings.TrimRight(s.endpoint.Path, "/")
	if s.c.PathStyle {
		u.Path = base + "/" + s.c.Bucket + "/" + key
	} else {
		u.Host = s.c.Bucket + "." + s.endpoint.Host
		u.Path = base + "/" + key
	}
	u.RawPath = ""
	return &u
}

// S3Error is an error response of the service.
type S3Error struct {
	Status  int
	Code    string
	Message string
}

func (e *S3Error) Error() string {
	return fmt.Sprintf("media: s3: %d %s: %s", e.Status, e.Code, e.Message)
}

func s3Failure(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	var x struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	_ = xml.Unmarshal(body, &x)
	if resp.StatusCode == http.StatusNotFound && (x.Code == "" || x.Code == "NoSuchKey" || x.Code == "NotFound") {
		return ErrNotFound
	}
	if x.Code == "" {
		x.Code = http.StatusText(resp.StatusCode)
	}
	return &S3Error{Status: resp.StatusCode, Code: x.Code, Message: x.Message}
}

// do signs and sends a request; a status outside ok is turned into an error and the body closed.
func (s *S3) do(req *http.Request, payloadHash string, ok ...int) (*http.Response, error) {
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signV4(req, payloadHash, s.creds, s.c.Region, "s3", s.c.Clock.Now())
	resp, err := s.c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	for _, st := range ok {
		if resp.StatusCode == st {
			return resp, nil
		}
	}
	defer func() { _ = resp.Body.Close() }()
	return nil, s3Failure(resp)
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	if !validKey(key) {
		return errBadKey(key)
	}
	hash := unsignedBody
	switch rs := r.(type) {
	case io.ReadSeeker:
		h := sha256.New()
		n, err := io.Copy(h, rs)
		if err != nil {
			return err
		}
		if n != size {
			return fmt.Errorf("media: put %s: got %d bytes, want %d", quote(key), n, size)
		}
		if _, err := rs.Seek(-n, io.SeekCurrent); err != nil {
			return err
		}
		hash = hex.EncodeToString(h.Sum(nil))
	default:
		if s.endpoint.Scheme == "http" { // unsigned payloads are only safe over TLS
			buf, err := io.ReadAll(io.LimitReader(r, size+1))
			if err != nil {
				return err
			}
			if int64(len(buf)) != size {
				return fmt.Errorf("media: put %s: got %d bytes, want %d", quote(key), len(buf), size)
			}
			sum := sha256.Sum256(buf)
			hash, r = hex.EncodeToString(sum[:]), bytes.NewReader(buf)
		}
	}
	body := io.LimitReader(r, size)
	if size == 0 {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(s.prefix+key).String(), body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	if mime == "" {
		mime = "application/octet-stream"
	}
	req.Header.Set("Content-Type", contentType(mime))
	req.Header.Set("Cache-Control", "public, max-age=31536000, immutable")
	// What a bucket or CDN serving the object directly can carry (S3 stores no CSP or nosniff): a type a
	// browser could render as a document is never shown inline.
	req.Header.Set("Content-Disposition", storedDisposition(mime))
	resp, err := s.do(req, hash, http.StatusOK)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}

func (s *S3) request(ctx context.Context, method, key string, hdr map[string]string, ok ...int) (*http.Response, error) {
	if !validKey(key) {
		return nil, errBadKey(key)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.objectURL(s.prefix+key).String(), nil)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return s.do(req, emptySHA256, ok...)
}

func infoOf(resp *http.Response) Info {
	info := Info{MIME: resp.Header.Get("Content-Type")}
	if t, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		info.ModTime = t.UTC()
	}
	info.Size = resp.ContentLength
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if _, total, ok := strings.Cut(cr, "/"); ok {
			if n, err := strconv.ParseInt(total, 10, 64); err == nil {
				info.Size = n
			}
		}
	}
	return info
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	resp, err := s.request(ctx, http.MethodGet, key, nil, http.StatusOK)
	if err != nil {
		return nil, Info{}, err
	}
	return resp.Body, infoOf(resp), nil
}

func (s *S3) GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, Info, error) {
	offset = max(offset, 0)
	if length <= 0 {
		info, err := s.Stat(ctx, key)
		return io.NopCloser(bytes.NewReader(nil)), info, err
	}
	rng := "bytes=" + strconv.FormatInt(offset, 10) + "-" + strconv.FormatInt(offset+length-1, 10)
	resp, err := s.request(ctx, http.MethodGet, key, map[string]string{"Range": rng}, http.StatusPartialContent, http.StatusOK, http.StatusRequestedRangeNotSatisfiable)
	if err != nil {
		return nil, Info{}, err
	}
	info := infoOf(resp)
	switch resp.StatusCode {
	case http.StatusRequestedRangeNotSatisfiable:
		_ = resp.Body.Close()
		info, err = s.Stat(ctx, key)
		return io.NopCloser(bytes.NewReader(nil)), info, err
	case http.StatusOK: // the service ignored the range
		if _, err := io.CopyN(io.Discard, resp.Body, offset); err != nil {
			_ = resp.Body.Close()
			return io.NopCloser(bytes.NewReader(nil)), info, nil
		}
		return limited{io.LimitReader(resp.Body, length), resp.Body}, info, nil
	}
	return resp.Body, info, nil
}

type limited struct {
	io.Reader
	c io.Closer
}

func (l limited) Close() error { return l.c.Close() }

// Stat treats 403 like 404: a caller without s3:ListBucket gets 403 for a missing key (S3 does not say that the
// key is absent). A real permission problem then shows when the object is written or read.
func (s *S3) Stat(ctx context.Context, key string) (Info, error) {
	resp, err := s.request(ctx, http.MethodHead, key, nil, http.StatusOK)
	var se *S3Error
	if errors.As(err, &se) && se.Status == http.StatusForbidden {
		return Info{}, ErrNotFound
	}
	if err != nil {
		return Info{}, err
	}
	_ = resp.Body.Close()
	return infoOf(resp), nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	resp, err := s.request(ctx, http.MethodDelete, key, nil, http.StatusNoContent, http.StatusOK, http.StatusNotFound)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (s *S3) List(ctx context.Context, prefix, after string, limit int) ([]Entry, error) {
	var out []Entry
	start := ""
	if after != "" {
		start = s.prefix + after
	}
	for len(out) < limit {
		q := url.Values{"list-type": {"2"}, "prefix": {s.prefix + prefix}, "max-keys": {strconv.Itoa(min(limit-len(out), 1000))}}
		if start != "" {
			q.Set("start-after", start)
		}
		u := s.objectURL("")
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := s.do(req, emptySHA256, http.StatusOK)
		if err != nil {
			return nil, err
		}
		var res struct {
			IsTruncated bool `xml:"IsTruncated"`
			Contents    []struct {
				Key          string `xml:"Key"`
				Size         int64  `xml:"Size"`
				LastModified string `xml:"LastModified"`
			} `xml:"Contents"`
		}
		err = xml.NewDecoder(resp.Body).Decode(&res)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("media: s3 list: %w", err)
		}
		for _, c := range res.Contents {
			e := Entry{Key: strings.TrimPrefix(c.Key, s.prefix), Size: c.Size}
			if t, err := time.Parse(time.RFC3339, c.LastModified); err == nil {
				e.ModTime = t.UTC()
			}
			out = append(out, e)
			start = c.Key
		}
		if !res.IsTruncated || len(res.Contents) == 0 {
			break
		}
	}
	return out, nil
}
