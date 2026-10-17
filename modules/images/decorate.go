package images

import (
	"encoding/json"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
)

// DecorateAsset adds `variants` to a raster image: for each preset the channel lists, the url, planned size,
// type and srcset of its renditions. Assets the engine cannot decode (SVG, PDF…) or whose size is unknown or
// above max_pixels get none.
func (m *module) DecorateAsset(ch *channel.Channel, file, asset map[string]any) {
	if m.media == nil || len(m.s.names) == 0 {
		return
	}
	mime, _ := asset["mime"].(string)
	sha, _ := file["id"].(string)
	name, _ := asset["name"].(string)
	srcW, srcH := intOf(asset["width"]), intOf(asset["height"])
	if sha == "" || !Decodable(mime) || srcW < 1 || srcH < 1 || int64(srcW)*int64(srcH) > m.s.maxPixels {
		return
	}
	focal := focalOf(asset["focal"])
	names := m.s.names
	if ch != nil {
		if l, ok := ch.Options[ChannelOption].([]string); ok {
			names = l
		}
	}
	out := map[string]any{}
	for _, n := range names {
		p, ok := m.s.presets[n]
		if !ok || n == AdminPreset {
			continue
		}
		box := Plan(srcW, srcH, focal, p, p.Width)
		_, typ := OutputFormat(p, mime)
		tok := focalFor(p, focal)
		pf, _ := parseFocal(tok)
		v := map[string]any{"url": m.variantURL(p, p.Width, tok, pf, sha, name, mime), "width": box.W, "height": box.H, "type": typ}
		var set []string
		seen := 0
		for _, w := range p.Allowed() {
			b := Plan(srcW, srcH, focal, p, w)
			if b.W <= seen {
				continue
			}
			seen = b.W
			set = append(set, m.variantURL(p, w, tok, pf, sha, name, mime)+" "+itoa(b.W)+"w")
		}
		if len(set) > 1 {
			v["srcset"] = strings.Join(set, ", ")
		}
		out[n] = v
	}
	if len(out) > 0 {
		asset["variants"] = out
	}
}

func (m *module) variantURL(p Preset, w int, tok string, focal *Focal, sha, name, mime string) string {
	if m.s.driver == driverImgproxy {
		return m.s.proxy.URL(sha, canonicalName(mime), p, w, focal, mime)
	}
	return m.builtinURL(p, w, tok, sha, name, mime)
}

func intOf(v any) int {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func floatOf(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func focalOf(v any) *Focal {
	m, _ := v.(map[string]any)
	x, okx := floatOf(m["x"])
	y, oky := floatOf(m["y"])
	if !okx || !oky {
		return nil
	}
	return &Focal{X: x, Y: y}
}
