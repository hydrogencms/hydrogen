package images

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/features/media"
)

const gcPage = 500

// gc is the images.gc job: it deletes stored variants no preset can request any more (the preset is gone, its
// version changed or the width is no longer allowed).
func (m *module) gc(ctx context.Context, _ *kernel.Services) error {
	if m.media == nil {
		return nil
	}
	after, removed := "", 0
	for {
		list, err := m.media.ListDerived(ctx, after, gcPage)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			break
		}
		for _, e := range list {
			after = e.Key
			sha, suffix, ok := media.SplitDerived(e.Key)
			if !ok || !m.stale(suffix) {
				continue
			}
			if err := m.media.DeleteDerived(ctx, sha, suffix); err != nil {
				return err
			}
			removed++
		}
	}
	if removed > 0 && m.log != nil {
		m.log.Info("images: removed stale variants", "count", removed)
	}
	return nil
}

// stale reports whether a derived file is not "<preset>-<v>-<w>-<focal>" of a preset, version, width and focal
// token that can be requested now.
func (m *module) stale(suffix string) bool {
	parts := strings.Split(suffix, "-")
	if len(parts) != 4 {
		return true
	}
	w, err := strconv.Atoi(parts[2])
	if err != nil {
		return true
	}
	p, ok := m.s.presets[parts[0]]
	if !ok || parts[1] != m.versions[p.Name] || !slices.Contains(p.Allowed(), w) {
		return true
	}
	f, ok := parseFocal(parts[3])
	return !ok || f != nil && p.fit() != Cover
}
