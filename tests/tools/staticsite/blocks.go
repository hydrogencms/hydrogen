package main

func hero(heading, sub, label, link string) map[string]any {
	return map[string]any{"type": "hero", "props": map[string]any{"heading": heading, "subheading": sub, "buttonLabel": label, "buttonLink": link}}
}

func text(heading string, nodes ...any) map[string]any {
	return map[string]any{"type": "text", "props": map[string]any{"heading": heading, "body": map[string]any{"type": "doc", "content": nodes}}}
}

func features(heading string, items ...any) map[string]any {
	return map[string]any{"type": "features", "props": map[string]any{"heading": heading}, "slots": map[string]any{"items": items}}
}

func feature(title, body string) map[string]any {
	return map[string]any{"type": "feature", "props": map[string]any{"title": title, "text": body}}
}

func callout(body, tone string) map[string]any {
	return map[string]any{"type": "callout", "props": map[string]any{"text": body, "tone": tone}}
}

func para(parts ...any) map[string]any {
	return map[string]any{"type": "paragraph", "content": parts}
}

func txt(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

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
