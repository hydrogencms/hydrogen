package wasmhooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/hooks"
)

// ABIVersion is the version in every request.
const ABIVersion = 1

// maxResponse caps a hook's stdout.
const maxResponse = 1 << 20

type request struct {
	V      int            `json:"v"`
	Event  hooks.Event    `json:"event"`
	Locale string         `json:"locale"`
	Item   requestItem    `json:"item"`
	Doc    map[string]any `json:"doc"`
}

type requestItem struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Key        string `json:"key"`
	Version    int    `json:"version"`
	MainLocale string `json:"mainLocale"`
}

func buildRequest(hc *hooks.Context) ([]byte, error) {
	r := request{V: ABIVersion, Event: hc.Event, Locale: hc.Locale, Doc: hc.Doc, Item: requestItem{
		ID: hc.Item.ID.String(), Type: hc.Item.Type, Key: hc.Item.Key, Version: hc.Item.Version, MainLocale: hc.Item.MainLocale}}
	return json.Marshal(r)
}

// response is a decoded hook response; absent keys stay nil.
type response struct {
	set     map[string]any
	errors  []fieldError
	block   string
	blocked bool // the error key was present, even if empty
}

type fieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func allowedKeys(e hooks.Event) []string {
	switch e {
	case hooks.BeforeValidate, hooks.BeforeSave:
		return []string{"set", "error"}
	case hooks.Validate:
		return []string{"errors"}
	case hooks.BeforePublish:
		return []string{"errors", "error"}
	case hooks.BeforeDelete, hooks.BeforeUnpublish:
		return []string{"error"}
	}
	return nil
}

// parseResponse checks a hook's stdout against the rules of its event.
func parseResponse(e hooks.Event, out []byte) (*response, error) {
	var res response
	if len(bytes.TrimSpace(out)) == 0 {
		return &res, nil
	}
	var top map[string]json.RawMessage
	if err := decode(out, &top); err != nil {
		return nil, fmt.Errorf("response is not a JSON object: %w", err)
	}
	allowed := allowedKeys(e)
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !slices.Contains(allowed, k) {
			return nil, fmt.Errorf("response key %q is not allowed for %s (allowed: %s)", k, e, allowedList(allowed))
		}
		raw := top[k]
		if isNull(raw) {
			continue
		}
		switch k {
		case "set":
			var set map[string]json.RawMessage
			if err := decode(raw, &set); err != nil {
				return nil, fmt.Errorf("response set must be an object: %w", err)
			}
			res.set = make(map[string]any, len(set))
			for name, v := range set {
				val, err := decodeValue(v)
				if err != nil {
					return nil, fmt.Errorf("response set.%s: %w", name, err)
				}
				res.set[name] = val
			}
		case "errors":
			var list []map[string]json.RawMessage
			if err := decode(raw, &list); err != nil {
				return nil, fmt.Errorf("response errors must be a list of {path, message}: %w", err)
			}
			for i, item := range list {
				fe, err := parseFieldError(item)
				if err != nil {
					return nil, fmt.Errorf("response errors[%d]: %w", i, err)
				}
				res.errors = append(res.errors, fe)
			}
		case "error":
			if err := json.Unmarshal(raw, &res.block); err != nil {
				return nil, errors.New("response error must be a string")
			}
			res.blocked = true
		}
	}
	return &res, nil
}

func parseFieldError(item map[string]json.RawMessage) (fieldError, error) {
	var fe fieldError
	for k, raw := range item {
		var dst *string
		switch k {
		case "path":
			dst = &fe.Path
		case "message":
			dst = &fe.Message
		case "code":
			dst = &fe.Code
		default:
			return fe, fmt.Errorf("unknown key %q (path, message, code)", k)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return fe, fmt.Errorf("%s must be a string", k)
		}
	}
	if fe.Message == "" {
		return fe, errors.New("message is required")
	}
	if fe.Code == "" {
		fe.Code = "hook"
	}
	return fe, nil
}

func allowedList(keys []string) string {
	if len(keys) == 0 {
		return "none: the response is ignored, write {} or nothing"
	}
	return strings.Join(keys, ", ")
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// decode parses one JSON value and rejects trailing data.
func decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after the JSON value")
	}
	return nil
}

// decodeValue keeps numbers exact (json.Number), as documents do everywhere else.
func decodeValue(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
