package channel

import (
	"net/url"
	"regexp"
)

var placeholder = regexp.MustCompile(`\{([^{}]*)\}`)

// PreviewPath fills the pattern a preview of the collection uses: its preview pattern, else its link pattern,
// else the preview fallback "*". {id}, {locale} and {collection} are built in; any other placeholder is read
// through field. Every value is escaped as one path segment, so a "/" inside it becomes %2F and cannot add
// segments. It reports false when there is no pattern, a field is missing, or a value is empty, "." or ".."
// (those would change the path). A pattern's literal text is used as written.
func (c *Channel) PreviewPath(collection, id, locale string, field func(string) (string, bool)) (string, bool) {
	pattern, ok := c.Preview[collection]
	if !ok {
		if pattern, ok = c.Links[collection]; !ok {
			if pattern, ok = c.Preview["*"]; !ok {
				return "", false
			}
		}
	}
	return fill(pattern, collection, id, locale, field)
}

// LinkPath fills the link pattern of the collection, with the rules of PreviewPath.
func (c *Channel) LinkPath(collection, id, locale string, field func(string) (string, bool)) (string, bool) {
	pattern, ok := c.Links[collection]
	if !ok {
		return "", false
	}
	return fill(pattern, collection, id, locale, field)
}

// PreviewURL is PreviewPath with the configured frontend prepended.
func (c *Channel) PreviewURL(collection, id, locale string, field func(string) (string, bool)) (string, bool) {
	p, ok := c.PreviewPath(collection, id, locale, field)
	if !ok {
		return "", false
	}
	return c.Frontend + p, true
}

func fill(pattern, collection, id, locale string, field func(string) (string, bool)) (string, bool) {
	missing := false
	out := placeholder.ReplaceAllStringFunc(pattern, func(m string) string {
		name := m[1 : len(m)-1]
		var v string
		switch name {
		case "id":
			v = id
		case "locale":
			v = locale
		case "collection":
			v = collection
		default:
			var found bool
			if field != nil {
				v, found = field(name)
			}
			if !found {
				missing = true
			}
		}
		if v == "" || v == "." || v == ".." {
			missing = true
		}
		return url.PathEscape(v)
	})
	return out, !missing
}
