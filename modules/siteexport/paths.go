package siteexport

import (
	"fmt"
	"mime"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/modules/site"
)

// rootDir is the directory of a channel's files: its host, else its name.
func rootDir(ch site.ExportChannel) string {
	if ch.Host != "" {
		return ch.Host
	}
	return ch.Name
}

// fileFor maps a site URL to the file a static host serves for it: /en/about -> en/about/index.html, the
// home page -> index.html, the 404 page -> 404.html (below the channel's prefix), and a path with a file
// extension that answers something else than HTML (sitemap.xml, llms.txt) -> that file.
func fileFor(ch site.ExportChannel, kind, rawURL, contentType string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.RawQuery != "" || u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return "", fmt.Errorf("site-export: %q cannot be written to a file (a path without a query is needed)", rawURL)
	}
	var rel string
	if kind == site.TargetNotFound {
		rel = path.Join(strings.Trim(ch.Prefix, "/"), "404.html")
	} else {
		p := strings.Trim(u.Path, "/")
		last := p[strings.LastIndex(p, "/")+1:]
		switch {
		case p == "":
			rel = "index.html"
		case !strings.HasPrefix(contentType, "text/html") && contentType != "" && !strings.Contains(last, "."):
			if ext := extensionOf(contentType); ext != "" {
				rel = p + ext // /feed answering XML: feed.xml, to be served at /feed with a rewrite
			} else {
				rel = p + "/index.html"
			}
		case strings.Contains(last, ".") && !strings.HasPrefix(contentType, "text/html"):
			rel = p
		default:
			rel = p + "/index.html"
		}
	}
	rel = rootDir(ch) + "/" + rel
	if !safeRel(rel) {
		return "", fmt.Errorf("site-export: %q maps to the unsafe file name %q", rawURL, rel)
	}
	return rel, nil
}

// assetFile is where an asset URL (/__hydrogen/assets/site.1a2b.css) goes below a channel's root.
func assetFile(root, assetURL string) (string, error) {
	rel := root + "/" + strings.TrimLeft(assetURL, "/")
	if !safeRel(rel) {
		return "", fmt.Errorf("site-export: asset %q maps to the unsafe file name %q", assetURL, rel)
	}
	return rel, nil
}

// extensionOf is the file extension for a non-HTML media type ("" when unknown).
func extensionOf(contentType string) string {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	switch {
	case mt == "application/xml", mt == "text/xml", strings.HasSuffix(mt, "+xml"):
		return ".xml"
	case mt == "application/json", strings.HasSuffix(mt, "+json"):
		return ".json"
	case mt == "text/plain":
		return ".txt"
	}
	exts, _ := mime.ExtensionsByType(mt)
	slices.Sort(exts)
	if len(exts) > 0 {
		return exts[0]
	}
	return ""
}
