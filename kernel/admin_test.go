package kernel

import (
	"net/http"
	"strings"
	"testing"
)

func TestAdminPrefixRules(t *testing.T) {
	for prefix, wantErr := range map[string]string{
		"/admin": "", "/cms/admin": "", "": "must start", "admin": "must start", "/admin/": "must start", "/": "must start",
		"/a//b": "must start", "/a\"b": "must start", "/a<b": "must start", "/a%b": "must start", "/a\\b": "must start", "/a/{x}": "must start", "/a.b/c-d_e~f": "",
		"/api": "collides", "/api/x": "collides", "/healthz": "collides", "/a b": "must start",
	} {
		c := AdminConfig{Enabled: true, Prefix: prefix}
		err := c.check()
		if wantErr == "" && err != nil || wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)) {
			t.Errorf("prefix %q: err = %v, want %q", prefix, err, wantErr)
		}
	}
}

func TestHandleReportsPatternCollisions(t *testing.T) {
	mux := http.NewServeMux()
	h := http.NotFoundHandler()
	if err := handle(mux, "/x/", h); err != nil {
		t.Fatal(err)
	}
	if err := handle(mux, "/x/", h); err == nil {
		t.Fatal("duplicate pattern accepted")
	}
}
