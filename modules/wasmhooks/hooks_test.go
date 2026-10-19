package wasmhooks

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/tetratelabs/wazero"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
)

const testSchema = `
types:
  article:
    name: "{title}"
    fields:
      title:          { type: string, max: 40 }
      body:           { type: text }
      readingMinutes: { type: number, integer: true, min: 0 }
      score:          { type: number }
      subtitle:       { type: string }
      notes:          { type: text }
      tags:           { type: list, of: { type: string } }
  page:
    fields:
      title: { type: string }
`

func buildSchema(t testing.TB) *schema.Schema {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": {Data: []byte(testSchema)}}, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// logs collects the module's log records.
type logs struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// newModule starts a module on hooks in dir; the shared cache compiles each fixture once.
func newModule(t testing.TB, cfg Config, dev bool) (*module, *logs) {
	t.Helper()
	m := &module{cfg: cfg, cache: sharedCache}
	if m.cfg.Dir == "" {
		m.cfg.Dir = DefaultDir
	}
	if m.cfg.Timeout == 0 {
		m.cfg.Timeout = 10 * time.Second // the default 200ms is for steady state; fixtures start a Go runtime
	}
	if err := m.init(dev); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	l := &logs{}
	m.log = slog.New(slog.NewTextHandler(l, nil))
	m.sc = buildSchema(t)
	return m, l
}

// oneHook runs the modes fixture named mode as a hook of event.
func oneHook(t testing.TB, mode string, event hooks.Event, mutate func(*Config)) (*module, hooks.Hook, *logs) {
	t.Helper()
	cfg := Config{Dir: hookDir(t, mode), Hooks: []HookConfig{{File: mode + ".wasm", Event: string(event)}}}
	if mutate != nil {
		mutate(&cfg)
	}
	m, l := newModule(t, cfg, false)
	return m, m.hooks()[0], l
}

func newCtx(event hooks.Event, doc map[string]any) *hooks.Context {
	return &hooks.Context{
		Context: context.Background(), Event: event, Locale: "en", Doc: doc, Errors: &schema.Report{},
		Item: hooks.Item{ID: ids.UUID{1, 2, 3}, Type: "article", Key: "k", Version: 3, MainLocale: "en"},
	}
}

func decodeDoc(t testing.TB, js string) map[string]any {
	t.Helper()
	d, err := schema.DecodeDoc([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRequestRoundTrip(t *testing.T) {
	events := []hooks.Event{hooks.BeforeValidate, hooks.BeforeSave}
	for _, ev := range events {
		_, h, _ := oneHook(t, "echo", ev, nil)
		hc := newCtx(ev, decodeDoc(t, `{"title":"Hi","readingMinutes":12,"score":1.5,"tags":["a"]}`))
		if err := h.Fn(hc); err != nil {
			t.Fatalf("%s: %v", ev, err)
		}
		var req struct {
			V      int
			Event  string
			Locale string
			Item   map[string]any
			Doc    map[string]any
		}
		if err := json.Unmarshal([]byte(hc.Doc["notes"].(string)), &req); err != nil {
			t.Fatal(err)
		}
		if req.V != 1 || req.Event != string(ev) || req.Locale != "en" || req.Item["type"] != "article" || req.Item["key"] != "k" ||
			req.Item["version"] != float64(3) || req.Item["mainLocale"] != "en" || req.Item["id"] != (ids.UUID{1, 2, 3}).String() {
			t.Errorf("%s: request %+v", ev, req)
		}
		if got := hc.Doc["notes"]; got == nil || req.Doc["title"] != "Hi" || req.Doc["readingMinutes"] != float64(12) {
			t.Errorf("%s: doc %+v", ev, req.Doc)
		}
	}
}

func TestRequestForEventsWithoutDoc(t *testing.T) {
	for _, ev := range []hooks.Event{hooks.BeforeDelete, hooks.BeforeUnpublish, hooks.AfterDelete, hooks.AfterUnpublish} {
		_, h, _ := oneHook(t, "emptyobj", ev, nil)
		hc := newCtx(ev, nil)
		if err := h.Fn(hc); err != nil {
			t.Errorf("%s: %v", ev, err)
		}
	}
	req, err := buildRequest(newCtx(hooks.BeforeDelete, nil))
	if err != nil || !strings.Contains(string(req), `"doc":null`) {
		t.Errorf("%s %v", req, err)
	}
}

func TestSetKeepsIntegers(t *testing.T) {
	_, h, _ := oneHook(t, "set", hooks.BeforeSave, nil)
	hc := newCtx(hooks.BeforeSave, decodeDoc(t, `{"title":"Hi","subtitle":"gone"}`))
	if err := h.Fn(hc); err != nil {
		t.Fatal(err)
	}
	if v, ok := hc.Doc["readingMinutes"].(json.Number); !ok || v != "4" {
		t.Errorf("readingMinutes = %#v", hc.Doc["readingMinutes"])
	}
	if v, ok := hc.Doc["score"].(json.Number); !ok || v != "2.5" {
		t.Errorf("score = %#v", hc.Doc["score"])
	}
	if _, ok := hc.Doc["subtitle"]; ok {
		t.Errorf("null should clear subtitle: %v", hc.Doc)
	}
	if tags, ok := hc.Doc["tags"].([]any); !ok || len(tags) != 2 {
		t.Errorf("tags = %#v", hc.Doc["tags"])
	}
	if hc.Doc["title"] != "Hi" {
		t.Errorf("title = %v", hc.Doc["title"])
	}
}

func TestSetNormalisesIntegralFloats(t *testing.T) {
	_, h, _ := oneHook(t, "setfloat", hooks.BeforeSave, nil)
	hc := newCtx(hooks.BeforeSave, map[string]any{})
	if err := h.Fn(hc); err != nil {
		t.Fatal(err)
	}
	if v := hc.Doc["readingMinutes"]; v != json.Number("5") {
		t.Errorf("readingMinutes = %#v", v)
	}
}

func TestSetErrors(t *testing.T) {
	for mode, want := range map[string]string{
		"setbad":     "invalid values",
		"setunknown": `"nope" is not a field of article`,
	} {
		_, h, _ := oneHook(t, mode, hooks.BeforeSave, nil)
		err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{}))
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), mode+".wasm (beforeSave)") {
			t.Errorf("%s: %v", mode, err)
		}
	}
	// beforeValidate leaves the check to the validation that follows
	_, h, _ := oneHook(t, "setbad", hooks.BeforeValidate, nil)
	hc := newCtx(hooks.BeforeValidate, map[string]any{})
	if err := h.Fn(hc); err != nil || hc.Doc["readingMinutes"] != "many" {
		t.Errorf("beforeValidate: %v %v", err, hc.Doc)
	}
}

func TestValidateErrors(t *testing.T) {
	for _, ev := range []hooks.Event{hooks.Validate, hooks.BeforePublish} {
		_, h, _ := oneHook(t, "validate", ev, nil)
		hc := newCtx(ev, map[string]any{"title": "x"})
		if err := h.Fn(hc); err != nil {
			t.Fatalf("%s: %v", ev, err)
		}
		errs := hc.Errors.Errors()
		if len(errs) != 2 || errs[0] != (schema.FieldError{Path: "title", Code: "hook", Message: "Too long"}) ||
			errs[1] != (schema.FieldError{Path: "notes", Code: "custom", Message: "Second"}) {
			t.Errorf("%s: %+v", ev, errs)
		}
	}
}

func TestBlockError(t *testing.T) {
	for _, ev := range []hooks.Event{hooks.BeforePublish, hooks.BeforeDelete, hooks.BeforeUnpublish, hooks.BeforeSave, hooks.BeforeValidate} {
		_, h, _ := oneHook(t, "block", ev, nil)
		err := h.Fn(newCtx(ev, map[string]any{}))
		if err == nil || !errors.Is(err, content.ErrBadRequest) || !strings.Contains(err.Error(), "Publishing is frozen") ||
			!strings.Contains(err.Error(), "block.wasm ("+string(ev)+")") {
			t.Errorf("%s: %v", ev, err)
		}
	}
}

func TestDisallowedKeys(t *testing.T) {
	cases := []struct {
		mode  string
		event hooks.Event
		want  string
	}{
		{"badkey", hooks.BeforeSave, `key "foo" is not allowed for beforeSave`},
		{"validate", hooks.BeforeSave, `key "errors" is not allowed for beforeSave`},
		{"set", hooks.Validate, `key "set" is not allowed for validate`},
		{"block", hooks.Validate, `key "error" is not allowed for validate`},
		{"set", hooks.BeforePublish, `key "set" is not allowed`},
		{"block", hooks.AfterPublish, `key "error" is not allowed for afterPublish`},
		{"set", hooks.BeforeDelete, `key "set" is not allowed for beforeDelete`},
		{"badjson", hooks.BeforeSave, "not a JSON object"},
	}
	for _, c := range cases {
		_, h, _ := oneHook(t, c.mode, c.event, nil)
		err := h.Fn(newCtx(c.event, map[string]any{}))
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), c.mode+".wasm ("+string(c.event)+")") {
			t.Errorf("%s/%s: %v", c.mode, c.event, err)
		}
	}
}

func TestEmptyResponseIsNoChange(t *testing.T) {
	for _, mode := range []string{"empty", "emptyobj"} {
		_, h, _ := oneHook(t, mode, hooks.BeforeSave, nil)
		hc := newCtx(hooks.BeforeSave, map[string]any{"title": "x"})
		if err := h.Fn(hc); err != nil || len(hc.Doc) != 1 || hc.Errors.OK() == false {
			t.Errorf("%s: %v %v", mode, err, hc.Doc)
		}
	}
}

func TestTimeout(t *testing.T) {
	_, h, _ := oneHook(t, "loop", hooks.BeforeSave, func(c *Config) { c.Timeout = 300 * time.Millisecond })
	start := time.Now()
	err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{}))
	if err == nil || !strings.Contains(err.Error(), "timed out after 300ms") || !strings.Contains(err.Error(), "loop.wasm (beforeSave)") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %s", d)
	}
}

func TestPerHookTimeout(t *testing.T) {
	_, h, _ := oneHook(t, "loop", hooks.BeforeSave, func(c *Config) { c.Hooks[0].Timeout = 250 * time.Millisecond })
	if err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{})); err == nil || !strings.Contains(err.Error(), "timed out after 250ms") {
		t.Fatalf("err = %v", err)
	}
}

func TestCancelledContext(t *testing.T) {
	_, h, _ := oneHook(t, "loop", hooks.BeforeSave, nil)
	hc := newCtx(hooks.BeforeSave, map[string]any{})
	ctx, cancel := context.WithCancel(context.Background())
	hc.Context = ctx
	time.AfterFunc(200*time.Millisecond, cancel)
	if err := h.Fn(hc); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestMemoryLimit(t *testing.T) {
	_, h, _ := oneHook(t, "grow", hooks.BeforeSave, func(c *Config) { c.Memory = 16 << 20 })
	err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{}))
	if err == nil || !strings.Contains(err.Error(), "exited with code") || !strings.Contains(err.Error(), "grow.wasm") {
		t.Fatalf("err = %v", err)
	}
	// the module itself needs more than the limit: reported when it is compiled, at start
	m := &module{cfg: Config{Dir: hookDir(t, "echo"), Memory: 1 << 20, Hooks: []HookConfig{{File: "echo.wasm", Event: "beforeSave"}}}, cache: sharedCache}
	if err := m.init(false); err == nil || !strings.Contains(err.Error(), "needs more memory than modules.wasmhooks.memory allows") {
		t.Errorf("small limit: %v", err)
	}
}

func TestNoFilesystemOrNetwork(t *testing.T) {
	for mode, want := range map[string][]string{
		"open": {"open: open /etc/passwd:", "readdir:"},
		"dial": {"dial:"},
	} {
		_, h, _ := oneHook(t, mode, hooks.BeforeSave, nil)
		hc := newCtx(hooks.BeforeSave, map[string]any{})
		if err := h.Fn(hc); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		notes, _ := hc.Doc["notes"].(string)
		for _, w := range want {
			if !strings.Contains(notes, w) {
				t.Errorf("%s: notes = %q", mode, notes)
			}
		}
		if strings.Contains(notes, "<nil>") {
			t.Errorf("%s: access was not refused: %q", mode, notes)
		}
	}
}

func TestEnvAndArgs(t *testing.T) {
	_, h, _ := oneHook(t, "env", hooks.BeforeSave, nil)
	hc := newCtx(hooks.BeforeSave, map[string]any{})
	t.Setenv("HYDROGEN_SECRET_FOR_TEST", "leak")
	if err := h.Fn(hc); err != nil {
		t.Fatal(err)
	}
	if got := hc.Doc["notes"]; got != "env=0 args=[env.wasm]" {
		t.Errorf("notes = %q", got)
	}
}

func TestClockAllowed(t *testing.T) {
	_, h, _ := oneHook(t, "clock", hooks.BeforeSave, nil)
	hc := newCtx(hooks.BeforeSave, map[string]any{})
	if err := h.Fn(hc); err != nil || hc.Doc["notes"] != "true" {
		t.Errorf("%v %v", err, hc.Doc)
	}
}

func TestExitAndPanic(t *testing.T) {
	_, h, _ := oneHook(t, "exit1", hooks.BeforeSave, nil)
	err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{}))
	if err == nil || !strings.Contains(err.Error(), "exited with code 1") || !strings.Contains(err.Error(), "boom: something broke") ||
		!strings.Contains(err.Error(), "exit1.wasm (beforeSave)") {
		t.Errorf("exit1: %v", err)
	}
	_, h, _ = oneHook(t, "panic", hooks.BeforeSave, nil)
	if err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{})); err == nil || !strings.Contains(err.Error(), "exited with code 2") {
		t.Errorf("panic: %v", err)
	}
}

func TestOutputLimits(t *testing.T) {
	_, h, _ := oneHook(t, "huge", hooks.BeforeSave, nil)
	err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{}))
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("huge: %v", err)
	}
	_, h, l := oneHook(t, "stderr", hooks.BeforeSave, nil)
	if err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{})); err != nil {
		t.Fatalf("stderr: %v", err)
	}
	out := l.String()
	if !strings.Contains(out, "stderr=eee") || !strings.Contains(out, "truncated=true") || len(out) > maxStderr+500 {
		t.Errorf("log is %d bytes: %.200s", len(out), out)
	}
}

func TestFreshInstancePerCall(t *testing.T) {
	_, h, _ := oneHook(t, "counter", hooks.BeforeSave, nil)
	for i := range 3 {
		hc := newCtx(hooks.BeforeSave, map[string]any{})
		if err := h.Fn(hc); err != nil || hc.Doc["notes"] != "1" {
			t.Fatalf("call %d: %v %v", i, err, hc.Doc)
		}
	}
}

func TestConcurrencyBound(t *testing.T) {
	for _, tc := range []struct {
		n   int
		min time.Duration
	}{{1, 300 * time.Millisecond}, {2, 0}} {
		_, h, _ := oneHook(t, "sleep", hooks.AfterPublish, func(c *Config) { c.Concurrency = tc.n })
		var wg sync.WaitGroup
		start := time.Now()
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := h.Fn(newCtx(hooks.AfterPublish, map[string]any{})); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if d := time.Since(start); d < tc.min {
			t.Errorf("concurrency %d: two 150ms calls finished in %s", tc.n, d)
		}
	}
}

func TestConcurrencyWaitHonoursDeadline(t *testing.T) {
	m, h, _ := oneHook(t, "sleep", hooks.AfterPublish, func(c *Config) { c.Concurrency = 1; c.Timeout = 100 * time.Millisecond })
	m.sem <- struct{}{} // the only slot is busy
	err := h.Fn(newCtx(hooks.AfterPublish, map[string]any{}))
	if err == nil || !strings.Contains(err.Error(), "no free slot") {
		t.Errorf("err = %v", err)
	}
}

func TestDevReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "const.wasm")
	copyFile(t, constV1, path)
	run := func(m *module) (string, error) {
		hc := newCtx(hooks.BeforeSave, map[string]any{})
		err := m.hooks()[0].Fn(hc)
		n, _ := hc.Doc["notes"].(string)
		return n, err
	}
	cfg := Config{Dir: dir, Hooks: []HookConfig{{File: "const.wasm", Event: "beforeSave"}}}

	m, _ := newModule(t, cfg, true)
	if n, err := run(m); err != nil || n != "v1" {
		t.Fatalf("v1: %q %v", n, err)
	}
	copyFile(t, constV2, path)
	if n, err := run(m); err != nil || n != "v2" {
		t.Fatalf("after reload: %q %v", n, err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(m); err == nil || !strings.Contains(err.Error(), "const.wasm") || !strings.Contains(err.Error(), "not a valid WebAssembly module") {
		t.Fatalf("broken file: %v", err)
	}
	copyFile(t, constV1, path)
	if n, err := run(m); err != nil || n != "v1" {
		t.Fatalf("fixed: %q %v", n, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := run(m); err == nil || !strings.Contains(err.Error(), "const.wasm") {
		t.Fatalf("removed: %v", err)
	}

	// outside dev the file is read once, at start
	copyFile(t, constV1, path)
	m, _ = newModule(t, cfg, false)
	copyFile(t, constV2, path)
	if n, err := run(m); err != nil || n != "v1" {
		t.Fatalf("no reload: %q %v", n, err)
	}
}

func TestRegistration(t *testing.T) {
	dir := hookDir(t, "echo", "block", "notify")
	cfg := Config{Dir: dir, Hooks: []HookConfig{
		{File: "echo.wasm", Event: "beforeSave", Types: []string{"page", "article"}},
		{File: "block.wasm", Event: "beforePublish"},
		{File: "notify.wasm", Event: "afterPublish", Async: true, Name: "notify"},
		{File: "notify.wasm", Event: "afterDelete", Async: true, Name: "bye", Types: []string{"article", "page"}},
	}}
	m, _ := newModule(t, cfg, false)
	type reg struct {
		Type, Event, Name string
		Async             bool
	}
	var got []reg
	var r hooks.Registry
	for _, h := range m.hooks() {
		if err := r.Add(h); err != nil {
			t.Fatal(err)
		}
		got = append(got, reg{h.Type, string(h.Event), h.Name, h.Async})
	}
	want := []reg{
		{"article", "beforeSave", "", false}, {"page", "beforeSave", "", false},
		{"", "beforePublish", "", false},
		{"", "afterPublish", "notify", true},
		{"article", "afterDelete", "bye.article", true}, {"page", "afterDelete", "bye.page", true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hook %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if len(m.files) != 3 {
		t.Errorf("%d compiled files, want 3 (shared per file)", len(m.files))
	}
	if n := len(r.Sync("page", hooks.BeforeSave)); n != 1 {
		t.Errorf("page beforeSave hooks: %d", n)
	}
}

func TestConfigErrors(t *testing.T) {
	dir := hookDir(t, "echo")
	if err := os.WriteFile(filepath.Join(dir, "junk.wasm"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	// a valid module without _start: a library, not a command
	if err := os.WriteFile(filepath.Join(dir, "lib.wasm"), []byte("\x00asm\x01\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	hook := func(h HookConfig) Config { return Config{Dir: dir, Hooks: []HookConfig{h}} }
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"missing file", hook(HookConfig{File: "gone.wasm", Event: "beforeSave"}), "gone.wasm"},
		{"no file", hook(HookConfig{Event: "beforeSave"}), "hooks[0]: file is required"},
		{"escaping file", hook(HookConfig{File: "../x.wasm", Event: "beforeSave"}), "must be a relative path inside"},
		{"bad event", hook(HookConfig{File: "echo.wasm", Event: "afterSave"}), `event "afterSave" is not one of`},
		{"async before", hook(HookConfig{File: "echo.wasm", Event: "beforeSave", Async: true, Name: "x"}), "beforeSave hooks cannot be async"},
		{"async no name", hook(HookConfig{File: "echo.wasm", Event: "afterPublish", Async: true}), "async hooks need a name"},
		{"bad name", hook(HookConfig{File: "echo.wasm", Event: "afterPublish", Async: true, Name: "a b"}), "name"},
		{"dup type", hook(HookConfig{File: "echo.wasm", Event: "beforeSave", Types: []string{"a", "a"}}), "types lists a type twice"},
		{"hook timeout", hook(HookConfig{File: "echo.wasm", Event: "beforeSave", Timeout: time.Minute}), "timeout 1m0s must be between"},
		{"timeout", Config{Dir: dir, Timeout: time.Minute}, "modules.wasmhooks.timeout"},
		{"memory", Config{Dir: dir, Memory: 1 << 30}, "memory must be between"},
		{"concurrency", Config{Dir: dir, Concurrency: -1}, "concurrency"},
		{"not wasm", hook(HookConfig{File: "junk.wasm", Event: "beforeSave"}), "junk.wasm: not a valid WebAssembly module"},
		{"library", hook(HookConfig{File: "lib.wasm", Event: "beforeSave"}), "lib.wasm: not a WASI command"},
	}
	for _, c := range cases {
		m := &module{cfg: c.cfg, cache: sharedCache}
		err := m.init(false)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "modules.wasmhooks") {
			t.Errorf("%s: %v (want %q)", c.name, err, c.want)
		}
	}
	dupAsync := Config{Dir: dir, Hooks: []HookConfig{
		{File: "echo.wasm", Event: "afterPublish", Async: true, Name: "n"}, {File: "echo.wasm", Event: "afterDelete", Async: true, Name: "n"}}}
	if err := (&module{cfg: dupAsync, cache: sharedCache}).init(false); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Errorf("duplicate async name: %v", err)
	}
}

func TestNoHooksIsInert(t *testing.T) {
	m := &module{cfg: defaultConfig()}
	if err := m.init(false); err != nil || m.rt != nil || len(m.hooks()) != 0 {
		t.Fatalf("%v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = io.Discard
}

func TestSleepIsInterrupted(t *testing.T) {
	_, h, _ := oneHook(t, "sleeplong", hooks.BeforeSave, func(c *Config) { c.Timeout = 300 * time.Millisecond })
	start := time.Now()
	err := h.Fn(newCtx(hooks.BeforeSave, map[string]any{}))
	if err == nil || !strings.Contains(err.Error(), "timed out after 300ms") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 600*time.Millisecond+500*time.Millisecond {
		t.Errorf("a 30s sleep held the call for %s", d)
	}
}

func TestEmptyErrorBlocks(t *testing.T) {
	_, h, _ := oneHook(t, "blockempty", hooks.BeforePublish, nil)
	err := h.Fn(newCtx(hooks.BeforePublish, map[string]any{}))
	if err == nil || !errors.Is(err, content.ErrBadRequest) || !strings.Contains(err.Error(), "blockempty.wasm (beforePublish): blocked") {
		t.Errorf("err = %v", err)
	}
}

func TestReloadIsNotTheCallsTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "const.wasm")
	copyFile(t, constV1, path)
	m := &module{cfg: Config{Dir: dir, Timeout: 100 * time.Millisecond, Hooks: []HookConfig{{File: "const.wasm", Event: "beforeSave"}}},
		cache: wazero.NewCompilationCache()} // a cold cache: the new file really compiles
	if err := m.init(true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	m.log, m.sc = slog.New(slog.NewTextHandler(io.Discard, nil)), buildSchema(t)
	h := m.hooks()[0]
	run := func() string {
		hc := newCtx(hooks.BeforeSave, map[string]any{})
		if err := h.Fn(hc); err != nil {
			t.Fatal(err)
		}
		return hc.Doc["notes"].(string)
	}
	if run() != "v1" {
		t.Fatal("v1")
	}
	old := m.files["const.wasm"].cur
	copyFile(t, constV2, path)
	start := time.Now()
	if run() != "v2" {
		t.Fatal("v2")
	}
	t.Logf("reload + call took %s with a 100ms call timeout", time.Since(start))
	if !old.closed || old.refs != 0 {
		t.Errorf("old module: closed=%v refs=%d", old.closed, old.refs)
	}
}

func TestReloadIsRateLimited(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "const.wasm")
	copyFile(t, constV1, path)
	m := &module{cfg: Config{Dir: dir, Timeout: 10 * time.Second, Hooks: []HookConfig{{File: "const.wasm", Event: "beforeSave"}}},
		cache: sharedCache, reloadEvery: time.Hour}
	if err := m.init(true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	m.log, m.sc = slog.New(slog.NewTextHandler(io.Discard, nil)), buildSchema(t)
	hc := newCtx(hooks.BeforeSave, map[string]any{})
	copyFile(t, constV2, path)
	if err := m.hooks()[0].Fn(hc); err != nil || hc.Doc["notes"] != "v1" {
		t.Errorf("%v %v", err, hc.Doc)
	}
}

func TestTableLimit(t *testing.T) {
	huge := []byte("\x00asm\x01\x00\x00\x00\x04\x06\x01\x70\x00\xc0\x9a\x0c")
	if err := checkTables(huge); err == nil || !strings.Contains(err.Error(), "200000 entries") {
		t.Errorf("huge table: %v", err)
	}
	small := []byte("\x00asm\x01\x00\x00\x00\x04\x04\x01\x70\x00\x0a")
	if err := checkTables(small); err != nil {
		t.Errorf("small table: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "t.wasm"), huge, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &module{cfg: Config{Dir: dir, Hooks: []HookConfig{{File: "t.wasm", Event: "beforeSave"}}}, cache: sharedCache}
	if err := m.init(false); err == nil || !strings.Contains(err.Error(), "t.wasm: a table starts with") {
		t.Errorf("init: %v", err)
	}
}

func TestGeneratedAsyncNamesAreUnique(t *testing.T) {
	c := Config{Hooks: []HookConfig{
		{File: "a.wasm", Event: "afterPublish", Async: true, Name: "n.x"},
		{File: "a.wasm", Event: "afterDelete", Async: true, Name: "n", Types: []string{"x", "y"}},
	}}
	if _, err := c.build(); err == nil || !strings.Contains(err.Error(), `consumer name "n.x" is already used by hooks[0]`) {
		t.Errorf("err = %v", err)
	}
}
