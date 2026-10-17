package media

import (
	"context"
	"io"
)

const maxSuffix = 56

// SplitDerived splits a storage key of the form "<sha>-<suffix>" (a derived file, such as an image variant);
// ok is false for any other key.
func SplitDerived(key string) (sha, suffix string, ok bool) {
	if len(key) < 66 || key[64] != '-' || !IsSha(key[:64]) {
		return "", "", false
	}
	suffix = key[65:]
	if !validSuffix(suffix) {
		return "", "", false
	}
	return key[:64], suffix, true
}

func validSuffix(s string) bool {
	if s == "" || len(s) > maxSuffix {
		return false
	}
	for i := range len(s) {
		switch c := s[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

func derivedKey(sha, suffix string) (string, error) {
	if !IsSha(sha) || !validSuffix(suffix) {
		return "", ErrNotFound
	}
	return sha + "-" + suffix, nil
}

// PutDerived stores a file derived from the upload sha (an image variant) under "<sha>-<suffix>"; suffix is
// 1–56 characters of [a-z0-9_-]. It is removed with its upload by garbage collection.
func (s *Service) PutDerived(ctx context.Context, sha, suffix string, r io.Reader, size int64, mime string) error {
	key, err := derivedKey(sha, suffix)
	if err != nil {
		return err
	}
	return s.o.Storage.Put(ctx, key, r, size, mime)
}

// OpenDerived opens a derived file; ErrNotFound when there is none (or the arguments are not valid).
func (s *Service) OpenDerived(ctx context.Context, sha, suffix string) (io.ReadCloser, Info, error) {
	key, err := derivedKey(sha, suffix)
	if err != nil {
		return nil, Info{}, err
	}
	return s.o.Storage.Get(ctx, key)
}

// DeleteDerived removes a derived file; a missing one is not an error, an invalid sha or suffix is.
func (s *Service) DeleteDerived(ctx context.Context, sha, suffix string) error {
	key, err := derivedKey(sha, suffix)
	if err != nil {
		return errBadKey(sha + "-" + suffix)
	}
	return s.o.Storage.Delete(ctx, key)
}

// ListDerived lists derived files in key order after `after` (a full key), up to limit.
func (s *Service) ListDerived(ctx context.Context, after string, limit int) ([]Entry, error) {
	var out []Entry
	for len(out) < limit {
		entries, err := s.o.Storage.List(ctx, "", after, gcPage)
		if err != nil {
			return out, err
		}
		if len(entries) == 0 {
			break
		}
		after = entries[len(entries)-1].Key
		for _, e := range entries {
			if _, _, ok := SplitDerived(e.Key); ok {
				out = append(out, e)
				if len(out) == limit {
					return out, nil
				}
			}
		}
	}
	return out, nil
}
