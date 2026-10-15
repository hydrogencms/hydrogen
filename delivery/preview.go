package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
)

// Preview token lifetime limits.
const (
	DefaultPreviewTTL = 15 * time.Minute
	MaxPreviewTTL     = time.Hour
)

var errPreviewToken = errors.New("delivery: invalid or expired preview token")

// claims are the signed contents of a preview token.
type claims struct {
	Channel string `json:"ch"`
	Item    string `json:"item,omitempty"`
	Exp     int64  `json:"exp"`
}

var b64 = base64.RawURLEncoding

// MintPreviewToken signs a token that lets the holder read draft content of one channel (and optionally of one
// item only) until it expires. ttl 0 means DefaultPreviewTTL; it may not exceed MaxPreviewTTL.
func (a *API) MintPreviewToken(chanName string, item ids.UUID, ttl time.Duration) (token string, expires time.Time, err error) {
	if _, ok := a.o.Channels.Get(chanName); !ok {
		return "", time.Time{}, fmt.Errorf("delivery: unknown channel %q", chanName)
	}
	if ttl == 0 {
		ttl = DefaultPreviewTTL
	}
	if ttl < 0 || ttl > MaxPreviewTTL {
		return "", time.Time{}, fmt.Errorf("delivery: preview ttl must be between 1s and %s", MaxPreviewTTL)
	}
	expires = a.o.Clock.Now().Add(ttl).UTC().Truncate(time.Second)
	cl := claims{Channel: chanName, Exp: expires.Unix()}
	if !item.IsZero() {
		cl.Item = item.String()
	}
	raw, err := json.Marshal(cl)
	if err != nil {
		return "", time.Time{}, err
	}
	payload := b64.EncodeToString(raw)
	return payload + "." + b64.EncodeToString(a.sign(payload)), expires, nil
}

func (a *API) sign(payload string) []byte {
	m := hmac.New(sha256.New, a.o.PreviewSecret)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// verifyPreview checks the signature (constant time) and the expiry.
func (a *API) verifyPreview(tok string) (*claims, error) {
	payload, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return nil, errPreviewToken
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.sign(payload)) {
		return nil, errPreviewToken
	}
	raw, err := b64.DecodeString(payload)
	if err != nil {
		return nil, errPreviewToken
	}
	var cl claims
	if err := json.Unmarshal(raw, &cl); err != nil || cl.Channel == "" {
		return nil, errPreviewToken
	}
	if a.o.Clock.Now().Unix() >= cl.Exp {
		return nil, errPreviewToken
	}
	return &cl, nil
}

func previewToken(r *http.Request) string {
	if v := r.Header[previewHeader]; len(v) > 0 {
		return v[0]
	}
	if r.URL.RawQuery == "" {
		return ""
	}
	return rawParam(r.URL.RawQuery, "preview")
}

// previews reports whether an item is covered by the request's preview token.
func (st *state) previews(id ids.UUID) bool {
	if st.preview == nil {
		return false
	}
	return st.preview.Item == "" || st.preview.Item == id.String()
}

type previewRequest struct {
	Channel string          `json:"channel"`
	Item    string          `json:"item"`
	TTL     json.RawMessage `json:"ttl"`
}

// PreviewTokenRoute is the management route POST /preview-tokens {channel, item?, ttl?} that mints preview
// tokens. ttl is a number of seconds or a Go duration string ("15m"); at most one hour, default 15 minutes.
func (a *API) PreviewTokenRoute() manage.Route {
	type m = map[string]any
	errRef := func(code string) m { return m{"$ref": "#/components/responses/" + code} }
	return manage.Route{
		Method: http.MethodPost, Path: "/preview-tokens", Summary: "Mint a delivery preview token",
		Handler: a.mintHandler,
		OpenAPI: m{
			"operationId": "createPreviewToken",
			"summary":     "Mint a delivery preview token",
			"description": "The token goes in X-Hydrogen-Preview (or ?preview=) on delivery requests of that channel; " +
				"they then show draft content, uncached. An item-scoped token previews only that item.",
			"requestBody": m{"required": true, "content": m{"application/json": m{"schema": m{
				"type": "object", "required": []any{"channel"}, "additionalProperties": false,
				"properties": m{
					"channel": m{"type": "string"},
					"item":    m{"type": "string", "format": "uuid"},
					"ttl": m{"oneOf": []any{m{"type": "integer", "minimum": 1, "maximum": 3600}, m{"type": "string"}},
						"description": "Seconds or a duration such as 15m; at most 1h, default 15m."},
				}}}}},
			"responses": m{
				"200": m{"description": "The token.", "content": m{"application/json": m{"schema": m{
					"type": "object", "required": []any{"token", "expiresAt"},
					"properties": m{"token": m{"type": "string"}, "expiresAt": m{"type": "string", "format": "date-time"}}}}}},
				"400": errRef("BadRequest"), "401": errRef("Unauthorized"), "413": errRef("TooLarge"), "415": errRef("UnsupportedMedia"),
			},
		},
	}
}

func (a *API) mintHandler(w http.ResponseWriter, r *http.Request) {
	var in previewRequest
	if err := manage.DecodeJSON(w, r, &in); err != nil {
		manage.Fail(w, r, err)
		return
	}
	var item ids.UUID
	if in.Item != "" {
		var err error
		if item, err = ids.Parse(in.Item); err != nil {
			manage.Fail(w, r, manage.BadRequest("item %q is not an item id", in.Item))
			return
		}
	}
	ttl, err := parseTTL(in.TTL)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if _, ok := a.o.Channels.Get(in.Channel); !ok {
		manage.Fail(w, r, manage.BadRequest("unknown channel %q", in.Channel))
		return
	}
	tok, exp, err := a.MintPreviewToken(in.Channel, item, ttl)
	if err != nil {
		manage.Fail(w, r, manage.BadRequest("%s", strings.TrimPrefix(err.Error(), "delivery: ")))
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"token": tok, "expiresAt": exp.Format(time.RFC3339)})
}

func parseTTL(raw json.RawMessage) (time.Duration, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var d time.Duration
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, manage.BadRequest("ttl: %v", err)
		}
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, manage.BadRequest("ttl %q is not a duration such as 15m", s)
		}
	} else {
		var secs float64
		if err := json.Unmarshal(raw, &secs); err != nil {
			return 0, manage.BadRequest("ttl must be seconds or a duration string")
		}
		d = time.Duration(secs * float64(time.Second))
	}
	if d < time.Second || d > MaxPreviewTTL {
		return 0, manage.BadRequest("ttl must be between 1s and %s", MaxPreviewTTL)
	}
	return d, nil
}
