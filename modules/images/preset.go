// Package images turns uploaded raster images into named, versioned variants (presets), with an in-process
// engine or through imgproxy.
package images

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// engineRev changes when the builtin engine's output changes, so cached variants and their URLs are renewed.
const engineRev = 1

// DefaultQuality is the encoder quality of a preset that sets none.
const DefaultQuality = 82

const (
	Contain = "contain"
	Cover   = "cover"
)

const (
	maxDim   = 8192
	minExtra = 16
)

var presetName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// Preset is a named set of transform parameters.
type Preset struct {
	Name    string
	Width   int
	Height  int    // 0: none
	Fit     string // "contain" | "cover"
	Widths  []int  // extra widths, ascending, < Width
	Format  string // "original" | "jpeg" | "png" | "webp" | "avif"
	Quality int
	Upscale bool
}

func (p Preset) fit() string {
	if p.Fit == "" {
		return Contain
	}
	return p.Fit
}

func (p Preset) format() string {
	if p.Format == "" {
		return "original"
	}
	return p.Format
}

func (p Preset) quality() int {
	if p.Quality <= 0 {
		return DefaultQuality
	}
	return p.Quality
}

// Allowed lists the widths a variant may be requested at: Widths, then Width, ascending.
func (p Preset) Allowed() []int {
	out := make([]int, 0, len(p.Widths)+1)
	out = append(out, p.Widths...)
	return append(out, p.Width)
}

// Version is 8 hex characters identifying the preset's parameters under a driver and engine revision. Unset
// fields count as their defaults.
func (p Preset) Version(driver string) string {
	ws := make([]string, len(p.Widths))
	for i, w := range p.Widths {
		ws[i] = strconv.Itoa(w)
	}
	canon := fmt.Sprintf("rev=%d;driver=%s;w=%d;h=%d;fit=%s;widths=%s;format=%s;q=%d;up=%t",
		engineRev, driver, p.Width, p.Height, p.fit(), strings.Join(ws, ","), p.format(), p.quality(), p.Upscale)
	sum := sha256.Sum256([]byte(canon))
	return hex.EncodeToString(sum[:])[:8]
}

// Validate checks the preset against the rules of its driver ("builtin" or "imgproxy").
func (p Preset) Validate(driver string) error {
	if driver != "builtin" && driver != "imgproxy" {
		return fmt.Errorf("unknown driver %q", driver)
	}
	if !presetName.MatchString(p.Name) {
		return fmt.Errorf("preset name %q must match %s", p.Name, presetName)
	}
	if p.Width < 1 || p.Width > maxDim {
		return fmt.Errorf("preset %s: width must be 1–%d", p.Name, maxDim)
	}
	if p.Height != 0 && (p.Height < 1 || p.Height > maxDim) {
		return fmt.Errorf("preset %s: height must be 1–%d", p.Name, maxDim)
	}
	switch p.fit() {
	case Contain:
	case Cover:
		if p.Height == 0 {
			return fmt.Errorf("preset %s: fit cover needs a height", p.Name)
		}
	default:
		return fmt.Errorf("preset %s: fit must be contain or cover", p.Name)
	}
	for i, w := range p.Widths {
		if w < minExtra || w >= p.Width {
			return fmt.Errorf("preset %s: widths must be in [%d, %d), got %d", p.Name, minExtra, p.Width, w)
		}
		if i > 0 && w <= p.Widths[i-1] {
			return fmt.Errorf("preset %s: widths must be ascending and unique", p.Name)
		}
	}
	switch p.format() {
	case "original", "jpeg", "png":
	case "webp", "avif":
		if driver != "imgproxy" {
			return fmt.Errorf("preset %s: format %s needs the imgproxy driver", p.Name, p.Format)
		}
	default:
		return fmt.Errorf("preset %s: unknown format %q", p.Name, p.Format)
	}
	if p.Quality < 0 || p.Quality > 100 {
		return fmt.Errorf("preset %s: quality must be 1–100", p.Name)
	}
	return nil
}

// OutputFormat is the file extension and media type of a variant of a source with type srcMIME. "original"
// keeps jpeg and png and turns anything else (gif, webp) into png.
func OutputFormat(p Preset, srcMIME string) (ext, mime string) {
	f := p.format()
	if f == "original" {
		if srcMIME == "image/jpeg" {
			f = "jpeg"
		} else {
			f = "png"
		}
	}
	switch f {
	case "jpeg":
		return "jpg", "image/jpeg"
	case "webp":
		return "webp", "image/webp"
	case "avif":
		return "avif", "image/avif"
	}
	return "png", "image/png"
}

// OutputName gives name the extension ext.
func OutputName(name, ext string) string {
	base := strings.TrimSuffix(name, path.Ext(name))
	if base == "" {
		base = "image"
	}
	return base + "." + ext
}
