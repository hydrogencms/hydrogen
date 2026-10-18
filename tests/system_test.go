package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func systemBuildInfo() (*debug.BuildInfo, bool) {
	return &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/acme", Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: "github.com/hydrogencms/hydrogen", Version: "v0.5.1"},
			{Path: "github.com/hydrogencms/hydrogen/modules/sqlite", Version: "v0.5.0"},
		},
	}, true
}

func TestSystemReport(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		cfg := mediaConfig(t, mediaKernelSchema, `manage: { token: "${env:TOKEN}" }`, map[string]string{"TOKEN": "system-token-0123456789"})
		var stdout bytes.Buffer
		a, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, DB: b.open(t, clk), Stderr: io.Discard, Stdout: &stdout,
			PasswordIterations: 1000, BuildInfo: systemBuildInfo}, sqlite.Module())
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		h, err := a.Handler()
		if err != nil {
			t.Fatal(err)
		}
		call := func(token, path string) *httptest.ResponseRecorder {
			req := httptest.NewRequest("GET", "/api/manage/v1"+path, nil)
			req.RemoteAddr = "127.0.0.1:50000"
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec
		}
		token := func(name string, policies ...string) string {
			t.Helper()
			var rp []access.RolePolicy
			for _, p := range policies {
				rp = append(rp, access.RolePolicy{Policy: p})
			}
			role, err := a.Services().Admin.CreateRole(ctx, access.RoleInput{Name: name, Policies: rp})
			if err != nil {
				t.Fatal(err)
			}
			_, secret, err := a.Services().Admin.CreateToken(ctx, access.TokenInput{Name: name, Roles: []ids.UUID{role.ID}})
			if err != nil {
				t.Fatal(err)
			}
			return secret
		}

		rec := call("system-token-0123456789", "/system")
		if rec.Code != http.StatusOK {
			t.Fatalf("config token: %d %s", rec.Code, rec.Body)
		}
		var got struct {
			Core    struct{ Version, Go string }
			Modules []struct {
				Name, Package, Module, Version string
				Requires                       []string
			}
			Database, Search string
			Media            bool
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Core.Version != "v0.5.1" || got.Core.Go != runtime.Version() {
			t.Errorf("core = %+v", got.Core)
		}
		if got.Database != b.name {
			t.Errorf("database = %q, backend %s", got.Database, b.name)
		}
		if got.Search != "database" || !got.Media { // the schema has an upload type
			t.Errorf("search = %q media = %v", got.Search, got.Media)
		}
		if len(got.Modules) != 1 || got.Modules[0].Name != "sqlite" || got.Modules[0].Module != "github.com/hydrogencms/hydrogen/modules/sqlite" ||
			got.Modules[0].Version != "v0.5.0" || got.Modules[0].Requires == nil {
			t.Errorf("modules = %+v", got.Modules)
		}

		// Without the policy: forbidden. With it, or as an administrator ("*"): allowed.
		if rec := call(token("reader", access.PolicyContentRead), "/system"); rec.Code != http.StatusForbidden {
			t.Errorf("reader: %d %s", rec.Code, rec.Body)
		}
		if rec := call(token("sys", access.PolicySystemRead), "/system"); rec.Code != http.StatusOK {
			t.Errorf("system.read: %d %s", rec.Code, rec.Body)
		}
		if rec := call(token("root", access.PolicyAll), "/system"); rec.Code != http.StatusOK {
			t.Errorf("administrator: %d %s", rec.Code, rec.Body)
		}
		if rec := call("", "/system"); rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous: %d", rec.Code)
		}

		if rec := call("system-token-0123456789", "/openapi.json"); !strings.Contains(rec.Body.String(), `"/system"`) ||
			!strings.Contains(rec.Body.String(), `"getSystem"`) {
			t.Error("openapi misses /system")
		}

		// The modules command prints the same list.
		c, ok := a.Command("modules")
		if !ok {
			t.Fatal("no modules command")
		}
		if err := c.Run(ctx, a, nil); err != nil {
			t.Fatal(err)
		}
		if out := stdout.String(); !strings.HasPrefix(out, "NAME") || !strings.Contains(out, "sqlite") ||
			!strings.Contains(out, "v0.5.0") || !strings.Contains(out, "github.com/hydrogencms/hydrogen/modules/sqlite") {
			t.Errorf("modules output:\n%s", out)
		}
	})
}

func TestAdministratorTemplateHoldsSystemRead(t *testing.T) {
	for _, tpl := range access.Templates() {
		if tpl.Name != "administrator" {
			continue
		}
		for _, p := range tpl.Policies {
			if p.Policy == access.PolicyAll {
				return
			}
		}
	}
	t.Fatal("the administrator template does not grant every policy, system.read included")
}

// The modules command lists the build before any database is opened: no adapter is registered here, so
// starting the app would fail.
func TestModulesCommandNeedsNoApp(t *testing.T) {
	var out, errOut bytes.Buffer
	err := hydrogen.Run(ctx, []string{"modules"}, func(b *hydrogen.Builder) {}, hydrogen.RunOptions{Stdout: &out, Stderr: &errOut})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "NAME") || !strings.Contains(out.String(), "project") {
		t.Errorf("output:\n%s", out.String())
	}
}
