package site

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// route is a parsed Route.
type route struct {
	Route
	segs  []seg
	param string // the {param}, "" for a listing
}

type seg struct{ lit, param string }

func parseRoute(r Route) (*route, error) {
	if !strings.HasPrefix(r.Path, "/") || r.Path == "/" || strings.HasSuffix(r.Path, "/") {
		return nil, fmt.Errorf("path %q must start with / and have no trailing slash (such as /blog/{slug})", r.Path)
	}
	if !typeNameRE.MatchString(r.Type) {
		return nil, fmt.Errorf("path %s: type %q is not a type name", r.Path, r.Type)
	}
	rt := &route{Route: r}
	for _, p := range strings.Split(r.Path[1:], "/") {
		if m := routeParam.FindStringSubmatch(p); m != nil {
			if rt.param != "" {
				return nil, fmt.Errorf("path %s: at most one {param}", r.Path)
			}
			rt.param = m[1]
			rt.segs = append(rt.segs, seg{param: m[1]})
			continue
		}
		if !segmentChar.MatchString(p) {
			return nil, fmt.Errorf("path %s: segment %q must be literal text or a whole {param}", r.Path, p)
		}
		rt.segs = append(rt.segs, seg{lit: p})
	}
	switch {
	case rt.param == "" && r.Template == "":
		return nil, fmt.Errorf("path %s: a listing needs a template", r.Path)
	case r.Template != "" && !templateRE.MatchString(r.Template):
		return nil, fmt.Errorf("path %s: template %q must be lower-case letters, digits, - and _", r.Path, r.Template)
	case r.Limit < 0 || r.Limit > 200:
		return nil, fmt.Errorf("path %s: limit must be from 1 to 200", r.Path)
	}
	if rt.Template == "" {
		rt.Template = r.Type
	}
	if rt.Limit == 0 {
		rt.Limit = 50
	}
	return rt, nil
}

func (r *route) templateName() string { return "collections/" + r.Template + ".html" }

// match reports the parameter value when a channel-relative path fits.
func (r *route) match(path string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) != len(r.segs) {
		return "", false
	}
	val := ""
	for i, sg := range r.segs {
		if sg.param == "" {
			if parts[i] != sg.lit {
				return "", false
			}
			continue
		}
		v, err := url.PathUnescape(parts[i])
		if err != nil || v == "" {
			return "", false
		}
		val = v
	}
	return val, true
}

// build fills the path for an item; false when the parameter has no usable value.
func (r *route) build(id string, fields map[string]any) (string, bool) {
	var sb strings.Builder
	for _, sg := range r.segs {
		sb.WriteByte('/')
		if sg.param == "" {
			sb.WriteString(sg.lit)
			continue
		}
		v := id
		if sg.param != "id" {
			s, ok := fields[sg.param].(string)
			if !ok || s == "" || s == "." || s == ".." {
				return "", false
			}
			v = s
		}
		sb.WriteString(url.PathEscape(v))
	}
	return sb.String(), true
}

func parseRoutes(list []Route) ([]*route, error) {
	var out []*route
	var errs []error
	for _, r := range list {
		rt, err := parseRoute(r)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, rt)
	}
	return out, errors.Join(errs...)
}

// matchPage reports the page number of a listing path: /blog is page 1, /blog/page/2 page 2.
func (r *route) matchPage(path string) (int, bool) {
	if r.param != "" {
		return 0, false
	}
	if path == r.Path {
		return 1, true
	}
	rest, ok := strings.CutPrefix(path, r.Path+"/page/")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 2 || n > 100000 || strconv.Itoa(n) != rest {
		return 0, false
	}
	return n, true
}
