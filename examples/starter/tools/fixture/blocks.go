package main

import "github.com/hydrogencms/hydrogen/core/ids"

// refs resolves item names to ids for links.
type refs map[string]ids.UUID

func (r refs) id(name string) string { return r[name].String() }

func hero(heading, sub, tone, cta string, link string) map[string]any {
	props := map[string]any{"heading": heading, "subheading": sub, "tone": tone, "ctaLabel": cta}
	if link != "" {
		props["ctaLink"] = link
	}
	return map[string]any{"type": "hero", "props": props}
}

func richText(nodes ...any) map[string]any {
	return map[string]any{"type": "richText", "props": map[string]any{"body": map[string]any{"type": "doc", "content": nodes}}}
}

func cards(heading, columns string, items ...any) map[string]any {
	return map[string]any{"type": "cards", "props": map[string]any{"heading": heading, "columns": columns, "space": "md"},
		"slots": map[string]any{"items": items}}
}

func card(title, text, link string) map[string]any {
	props := map[string]any{"title": title, "text": text}
	if link != "" {
		props["link"] = link
	}
	return map[string]any{"type": "card", "props": props}
}

func callout(text, tone string) map[string]any {
	return map[string]any{"type": "callout", "props": map[string]any{"text": text, "tone": tone}}
}

func image(src, alt, caption string) map[string]any {
	return map[string]any{"type": "image", "props": map[string]any{"src": src, "alt": alt, "caption": caption}}
}

func shared(item string) map[string]any {
	return map[string]any{"type": "shared", "props": map[string]any{"item": item}}
}

func para(parts ...any) map[string]any {
	return map[string]any{"type": "paragraph", "content": parts}
}

func heading(level int, text string) map[string]any {
	return map[string]any{"type": "heading", "attrs": map[string]any{"level": level}, "content": []any{txt(text)}}
}

func txt(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func bold(s string) map[string]any {
	return map[string]any{"type": "text", "text": s, "marks": []any{map[string]any{"type": "bold"}}}
}

// link is text linking to another item.
func link(s, item string) map[string]any {
	return map[string]any{"type": "text", "text": s,
		"marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "item:" + item}}}}
}

func bullets(items ...string) map[string]any {
	list := make([]any, len(items))
	for i, s := range items {
		list[i] = map[string]any{"type": "listItem", "content": []any{para(txt(s))}}
	}
	return map[string]any{"type": "bulletList", "content": list}
}

func pic(n string) string { return "https://picsum.photos/seed/hydrogen-" + n + "/1200/600" }
