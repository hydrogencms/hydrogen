package channel

import (
	"net/url"
	"regexp"
)

var placeholder = regexp.MustCompile(`\{([^{}]*)\}`)

// PreviewURL fills the channel's preview pattern for a collection ("*" as fallback). {id}, {locale} and
// {collection} are built in; any other placeholder is read through field. Every value is escaped as one path
// segment, so a "/" inside it becomes %2F and cannot add segments. It reports false when there is no pattern, a
// field is missing, or a value is empty, "." or ".." (those would change the path). A configured frontend is
// prepended, and a pattern's literal text is used as written.
func (c *Channel) PreviewURL(collection, id, locale string, field func(string) (string, bool)) (string, bool) {
	pattern, ok := c.Preview[collection]
	if !ok {
		if pattern, ok = c.Preview["*"]; !ok {
			return "", false
		}
	}
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
	if missing {
		return "", false
	}
	return c.Frontend + out, true
}
