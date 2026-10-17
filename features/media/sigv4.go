package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// AWS Signature Version 4 for header-based authentication, enough for object storage requests. Paths are
// encoded once (S3's rule); the signed headers are host, date, content-type, content-md5, range and every
// x-amz-* header present on the request.

const (
	sigAlgorithm  = "AWS4-HMAC-SHA256"
	amzDateFormat = "20060102T150405Z"
	emptySHA256   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	unsignedBody  = "UNSIGNED-PAYLOAD"
)

type credentials struct {
	AccessKey, SecretKey, SessionToken string
}

// signV4 sets X-Amz-Date (and X-Amz-Security-Token) and the Authorization header on req. payloadHash is the
// hex SHA-256 of the body, or unsignedBody; callers that send x-amz-content-sha256 set it before signing.
func signV4(req *http.Request, payloadHash string, c credentials, region, service string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format(amzDateFormat)
	req.Header.Set("X-Amz-Date", amzDate)
	if c.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.SessionToken)
	}
	canonical, signed := canonicalRequest(req, payloadHash)
	scope := scopeOf(now, region, service)
	sig := hex.EncodeToString(hmacSHA256(signingKey(c.SecretKey, now, region, service), stringToSign(amzDate, scope, canonical)))
	req.Header.Set("Authorization", sigAlgorithm+" Credential="+c.AccessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}

func scopeOf(t time.Time, region, service string) string {
	return t.UTC().Format("20060102") + "/" + region + "/" + service + "/aws4_request"
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func signingKey(secret string, t time.Time, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), t.UTC().Format("20060102"))
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	return hmacSHA256(k, "aws4_request")
}

func stringToSign(amzDate, scope, canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return sigAlgorithm + "\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])
}

func signedHeader(name string) bool {
	switch name {
	case "host", "date", "content-type", "content-md5", "range":
		return true
	}
	return strings.HasPrefix(name, "x-amz-")
}

// canonicalRequest returns the canonical request and the signed header names ("host;x-amz-date").
func canonicalRequest(req *http.Request, payloadHash string) (canonical, signedHeaders string) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": host}
	for name, vals := range req.Header {
		n := strings.ToLower(name)
		if !signedHeader(n) || n == "host" {
			continue
		}
		clean := make([]string, len(vals))
		for i, v := range vals {
			clean[i] = strings.Join(strings.Fields(v), " ")
		}
		headers[n] = strings.Join(clean, ",")
	}
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	slices.Sort(names)
	var b strings.Builder
	b.WriteString(req.Method)
	b.WriteByte('\n')
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	b.WriteString(uriEncode(path, false))
	b.WriteByte('\n')
	b.WriteString(canonicalQuery(req.URL.Query()))
	b.WriteByte('\n')
	for _, n := range names {
		b.WriteString(n + ":" + strings.TrimSpace(headers[n]) + "\n")
	}
	b.WriteByte('\n')
	signedHeaders = strings.Join(names, ";")
	b.WriteString(signedHeaders)
	b.WriteByte('\n')
	b.WriteString(payloadHash)
	return b.String(), signedHeaders
}

func canonicalQuery(q url.Values) string {
	type kv struct{ k, v string }
	var pairs []kv
	for k, vs := range q {
		for _, v := range vs {
			pairs = append(pairs, kv{uriEncode(k, true), uriEncode(v, true)})
		}
	}
	slices.SortFunc(pairs, func(a, b kv) int {
		if c := strings.Compare(a.k, b.k); c != 0 {
			return c
		}
		return strings.Compare(a.v, b.v)
	})
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.k + "=" + p.v
	}
	return strings.Join(parts, "&")
}

// uriEncode percent-encodes everything but the unreserved characters (and '/' unless encodeSlash).
func uriEncode(s string, encodeSlash bool) string {
	const hexUpper = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexUpper[c>>4])
			b.WriteByte(hexUpper[c&15])
		}
	}
	return b.String()
}
