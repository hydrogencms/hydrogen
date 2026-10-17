package manage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
)

// buildCaches precomputes the type responses and the OpenAPI document; the schema is immutable.
func (a *api) buildCaches(extra []Route) error {
	raw, err := a.sc.MarshalJSON()
	if err != nil {
		return fmt.Errorf("manage: encode schema: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var compiled struct {
		Types  []map[string]any `json:"types"`
		Blocks []map[string]any `json:"blocks"`
		Tokens []map[string]any `json:"tokens"`
	}
	if err := dec.Decode(&compiled); err != nil {
		return fmt.Errorf("manage: decode schema: %w", err)
	}
	if len(compiled.Types) != len(a.sc.Types()) {
		return fmt.Errorf("manage: compiled schema has %d types, want %d", len(compiled.Types), len(a.sc.Types()))
	}
	summaries := make([]any, 0, len(compiled.Types))
	a.typeJSON = make(map[string][]byte, len(compiled.Types))
	for i, t := range a.sc.Types() {
		ct := compiled.Types[i]
		sum := map[string]any{
			"name": t.Name, "label": t.Label, "description": t.Description,
			"singleton": t.Singleton, "page": t.Page, "review": t.Review, "fields": ct["fields"],
		}
		for _, k := range []string{"admin", "namePattern", "options", "upload"} {
			if v, ok := ct[k]; ok {
				sum[k] = v
			}
		}
		summaries = append(summaries, sum)
		if a.typeJSON[t.Name], err = encode(map[string]any{"type": ct, "jsonSchema": a.sc.JSONSchema(t)}); err != nil {
			return fmt.Errorf("manage: encode type %s: %w", t.Name, err)
		}
	}
	if a.typesJSON, err = encode(summaries); err != nil {
		return err
	}
	if compiled.Blocks == nil {
		compiled.Blocks = []map[string]any{}
	}
	if compiled.Tokens == nil {
		compiled.Tokens = []map[string]any{}
	}
	if a.blocksJSON, err = encode(map[string]any{"blocks": compiled.Blocks, "tokens": compiled.Tokens}); err != nil {
		return err
	}
	if a.channelsJSON, err = encodeChannels(a.channels); err != nil {
		return err
	}
	if a.openapi, err = encode(OpenAPI(a.sc, extra...)); err != nil {
		return fmt.Errorf("manage: encode openapi: %w", err)
	}
	sum := sha256.Sum256(a.openapi)
	a.etag = `"` + hex.EncodeToString(sum[:8]) + `"`
	return nil
}

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (a *api) writeRaw(w http.ResponseWriter, b []byte) {
	h := w.Header()
	h.Set("Content-Type", jsonType)
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-store")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(b); err != nil {
		a.log.Debug("manage: write response", "err", err)
	}
}

func (a *api) listTypes(w http.ResponseWriter, _ *http.Request) error {
	a.writeRaw(w, a.typesJSON)
	return nil
}

// listBlocks answers {"blocks":[…],"tokens":[…]}: the compiled block types (props as compiled fields, slots,
// labels, icons) and the design token sets.
func (a *api) listBlocks(w http.ResponseWriter, _ *http.Request) error {
	a.writeRaw(w, a.blocksJSON)
	return nil
}

func (a *api) getType(w http.ResponseWriter, r *http.Request) error {
	b, ok := a.typeJSON[r.PathValue("type")]
	if !ok {
		return NotFound("unknown type %q", r.PathValue("type"))
	}
	a.writeRaw(w, b)
	return nil
}

func (a *api) serveOpenAPI(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("ETag", a.etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == a.etag {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	a.writeRaw(w, a.openapi)
	return nil
}
