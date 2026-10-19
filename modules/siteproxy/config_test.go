package siteproxy

import (
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cfg   Config
		fs    bool
		want  string
		wmode mode
	}{
		{"static dir", Config{Static: StaticConfig{Dir: "out"}}, false, "", modeStatic},
		{"embedded fs", Config{}, true, "", modeStatic},
		{"ssr", Config{SSR: SSRConfig{Command: []string{"node", "s.js"}}}, false, "", modeSSR},
		{"nothing", Config{}, false, "set static.dir or ssr.command", 0},
		{"both", Config{Static: StaticConfig{Dir: "out"}, SSR: SSRConfig{Command: []string{"x"}}}, false, "both set", 0},
		{"fs and dir", Config{Static: StaticConfig{Dir: "out"}}, true, "embedded file system", 0},
		{"bad immutable", Config{Static: StaticConfig{Dir: "o", Immutable: []string{"static/*"}}}, false, "immutable[0]", 0},
		{"bad health", Config{SSR: SSRConfig{Command: []string{"x"}, Health: "healthz"}}, false, "ssr.health", 0},
		{"public host", Config{SSR: SSRConfig{Command: []string{"x"}, Host: "0.0.0.0"}}, false, "ssr.host", 0},
		{"localhost", Config{SSR: SSRConfig{Command: []string{"x"}, Host: "localhost"}}, false, "", modeSSR},
		{"backoff order", Config{SSR: SSRConfig{Command: []string{"x"}, RestartBackoff: 5e9, RestartBackoffMax: 1e9}}, false, "restart_backoff_max", 0},
	} {
		m, err := tc.cfg.validate(tc.fs)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
		if tc.want == "" && m != tc.wmode {
			t.Errorf("%s: mode %v", tc.name, m)
		}
	}
	c := Config{SSR: SSRConfig{Command: []string{"x"}}}
	if _, err := c.validate(false); err != nil || c.SSR.Health != "/" || c.SSR.StartTimeout == 0 || c.Timeout == 0 {
		t.Errorf("defaults: %v %+v", err, c)
	}
}
