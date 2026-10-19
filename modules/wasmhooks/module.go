// Package wasmhooks runs content hooks written in any language that compiles to a WASI preview 1 command module
// (Go, Rust, TinyGo, JavaScript through Javy, …). Each configured file is compiled once at start and
// instantiated afresh for every call, inside wazero's sandbox: no file system, environment or network, a memory
// limit and a deadline. See docs/wasm-hooks.md.
package wasmhooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/schema"
)

// Module returns the WebAssembly hooks module.
func Module() kernel.Module { return &module{cfg: defaultConfig(), reloadEvery: reloadEvery} }

// reloadEvery is how often a dev server looks at a hook file for changes.
const reloadEvery = 500 * time.Millisecond

type module struct {
	cfg         Config
	s           *setup
	dev         bool
	rt          wazero.Runtime
	reloadEvery time.Duration
	cache       wazero.CompilationCache // tests share one to compile each fixture once
	files       map[string]*wasmFile
	sem         chan struct{}
	log         *slog.Logger
	sc          *schema.Schema
}

func (*module) Name() string  { return "wasmhooks" }
func (m *module) Config() any { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	if err := m.init(r.Env() == config.DevEnv); err != nil {
		return err
	}
	for _, h := range m.hooks() {
		if err := r.AddHook(h); err != nil {
			return fmt.Errorf("modules.wasmhooks: %s: %w", h.Name, err)
		}
	}
	return nil
}

// init validates the config and compiles the hook files, so a broken project fails at config:check and start.
func (m *module) init(dev bool) error {
	s, err := m.cfg.build()
	if err != nil {
		return err
	}
	m.s, m.dev, m.log = s, dev, slog.Default()
	if len(s.hooks) == 0 {
		return nil
	}
	return m.open(context.Background())
}

// hooks lists one hook per configured (file, type).
func (m *module) hooks() []hooks.Hook {
	var out []hooks.Hook
	for _, spec := range m.s.hooks {
		f := m.files[spec.file]
		types := spec.types
		if len(types) == 0 {
			types = []string{""}
		}
		for _, typ := range types {
			h := hooks.Hook{Type: typ, Event: spec.event, Async: spec.async, Name: spec.name, Fn: m.hookFunc(f, spec)}
			if spec.async && len(types) > 1 {
				h.Name += "." + typ
			}
			out = append(out, h)
		}
	}
	return out
}

// open starts the runtime and compiles every distinct file.
func (m *module) open(ctx context.Context) error {
	rc := wazero.NewRuntimeConfig().WithMemoryLimitPages(m.s.pages).WithCloseOnContextDone(true)
	if m.cache == nil {
		m.cache = wazero.NewCompilationCache()
	}
	m.rt = wazero.NewRuntimeWithConfig(ctx, rc.WithCompilationCache(m.cache))
	wasi_snapshot_preview1.MustInstantiate(ctx, m.rt)
	m.sem = make(chan struct{}, m.s.concurrency)
	m.files = map[string]*wasmFile{}
	var errs []error
	for _, spec := range m.s.hooks {
		if _, done := m.files[spec.file]; done {
			continue
		}
		f := &wasmFile{path: filepath.Join(m.s.dir, filepath.FromSlash(spec.file)), name: spec.file, every: m.reloadEvery}
		m.files[spec.file] = f
		if _, release, err := f.get(ctx, m.rt, false); err != nil {
			errs = append(errs, err)
		} else {
			release()
		}
	}
	if err := errors.Join(errs...); err != nil {
		err = fmt.Errorf("modules.wasmhooks: %w", err)
		_ = m.rt.Close(ctx)
		m.rt = nil
		return err
	}
	return nil
}

func (m *module) Boot(_ context.Context, s *kernel.Services) error {
	m.log, m.sc = s.Log, s.Schema
	var errs []error
	for _, spec := range m.s.hooks {
		for _, typ := range spec.types {
			if _, ok := s.Schema.Type(typ); !ok {
				errs = append(errs, fmt.Errorf("modules.wasmhooks: %s (%s): unknown content type %q", spec.file, spec.event, typ))
			}
		}
	}
	return errors.Join(errs...)
}

func (m *module) Stop(ctx context.Context) error {
	if m.rt == nil {
		return nil
	}
	return m.rt.Close(ctx)
}

// wasmFile is a hook file and its compiled form.
type wasmFile struct {
	path, name string
	every      time.Duration // dev reload: how often the file is stat'ed at most

	mu      sync.Mutex
	cur     *modRef
	mod     int64 // mtime (ns) and size of the compiled bytes
	size    int64
	checked time.Time
	err     error // last failed reload, reported until the next check succeeds
}

// modRef is a compiled module that stays open while calls use it, and is closed once it is replaced and idle.
type modRef struct {
	cm      wazero.CompiledModule
	refs    int
	retired bool
	closed  bool
}

// get returns the current module with a reference the caller must release. With reload (dev) it stats the file
// (at most once per f.every) and recompiles it when it changed; a failed recompile is reported until the file is
// fixed.
func (f *wasmFile) get(ctx context.Context, rt wazero.Runtime, reload bool) (*modRef, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cur == nil || reload {
		if err := f.refresh(ctx, rt); err != nil {
			return nil, nil, err
		}
	}
	ref := f.cur
	ref.refs++
	return ref, func() { f.release(ctx, ref) }, nil
}

func (f *wasmFile) refresh(ctx context.Context, rt wazero.Runtime) error {
	now := time.Now()
	if f.cur != nil && f.every > 0 && now.Sub(f.checked) < f.every {
		return f.err
	}
	f.checked = now
	f.err = nil
	st, err := os.Stat(f.path)
	if err != nil {
		f.err = fmt.Errorf("hook file %s: %w", f.name, err)
		return f.err
	}
	if f.cur != nil && st.ModTime().UnixNano() == f.mod && st.Size() == f.size {
		return nil
	}
	bin, err := os.ReadFile(f.path)
	if err != nil {
		f.err = fmt.Errorf("hook file %s: %w", f.name, err)
		return f.err
	}
	cm, err := compileCommand(ctx, rt, bin)
	if err != nil {
		f.err = fmt.Errorf("hook file %s: %w", f.name, err)
		return f.err
	}
	if old := f.cur; old != nil {
		old.retired = true
		f.closeIfIdle(ctx, old)
	}
	f.cur, f.mod, f.size = &modRef{cm: cm}, st.ModTime().UnixNano(), st.Size()
	return nil
}

func (f *wasmFile) release(ctx context.Context, ref *modRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ref.refs--
	f.closeIfIdle(ctx, ref)
}

func (f *wasmFile) closeIfIdle(ctx context.Context, ref *modRef) {
	if ref.retired && ref.refs == 0 && !ref.closed {
		ref.closed = true
		_ = ref.cm.Close(context.WithoutCancel(ctx))
	}
}

// maxTableMin bounds the initial size of a module's tables: instantiating allocates them up front.
const maxTableMin = 100_000

// checkTables reads the table section of a binary module and rejects huge initial tables. A binary it cannot
// parse is left to the compiler to report.
func checkTables(bin []byte) error {
	if len(bin) < 8 {
		return nil
	}
	for p := 8; p < len(bin); {
		id := bin[p]
		size, n := leb(bin[p+1:])
		if n == 0 || p+1+n+int(size) > len(bin) {
			return nil
		}
		body := bin[p+1+n : p+1+n+int(size)]
		p += 1 + n + int(size)
		if id != 4 {
			continue
		}
		count, n := leb(body)
		body = body[n:]
		for range count {
			if len(body) == 0 {
				return nil
			}
			hasInit := body[0] == 0x40 // table with an initialiser: 0x40 0x00 reftype limits expr
			if hasInit {
				if len(body) < 3 {
					return nil
				}
				body = body[2:]
			}
			if len(body) < 2 {
				return nil
			}
			flag := body[1]
			body = body[2:]
			min, n := leb(body)
			if n == 0 {
				return nil
			}
			body = body[n:]
			if min > maxTableMin {
				return fmt.Errorf("a table starts with %d entries (the limit is %d)", min, maxTableMin)
			}
			if flag&1 != 0 {
				_, n = leb(body)
				if n == 0 {
					return nil
				}
				body = body[n:]
			}
			if hasInit {
				i := bytes.IndexByte(body, 0x0b)
				if i < 0 {
					return nil
				}
				body = body[i+1:]
			}
		}
	}
	return nil
}

// leb decodes an unsigned LEB128 value and the bytes it used (0 when truncated).
func leb(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}

// compileCommand compiles bin and checks that it is a WASI preview 1 command.
func compileCommand(ctx context.Context, rt wazero.Runtime, bin []byte) (wazero.CompiledModule, error) {
	if err := checkTables(bin); err != nil {
		return nil, err
	}
	cm, err := rt.CompileModule(ctx, bin)
	if err != nil {
		if strings.Contains(err.Error(), "over limit") {
			return nil, fmt.Errorf("needs more memory than modules.wasmhooks.memory allows: %w", err)
		}
		return nil, fmt.Errorf("not a valid WebAssembly module: %w", err)
	}
	if _, ok := cm.ExportedFunctions()["_start"]; !ok {
		_ = cm.Close(ctx)
		return nil, errors.New("not a WASI command: it exports no _start function (build it for wasip1 as a program, not a library)")
	}
	var bad []string
	for _, fn := range cm.ImportedFunctions() {
		if mod, name, _ := fn.Import(); mod != wasi_snapshot_preview1.ModuleName && !slices.Contains(bad, mod+"."+name) {
			bad = append(bad, mod+"."+name)
		}
	}
	if len(bad) > 0 {
		_ = cm.Close(ctx)
		return nil, fmt.Errorf("imports %v, but only wasi_snapshot_preview1 is available", bad)
	}
	return cm, nil
}
