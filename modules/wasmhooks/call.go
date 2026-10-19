package wasmhooks

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/sys"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/schema"
)

// maxStderr is how much of a call's stderr is kept for the log.
const maxStderr = 4 << 10

// compileTimeout bounds loading a changed hook file, outside the call's own deadline.
const compileTimeout = 30 * time.Second

// sleeper is the guest's sleep, cut short when the call ends (the runtime cannot interrupt a host call itself).
func sleeper(ctx context.Context) sys.Nanosleep {
	return func(ns int64) {
		t := time.NewTimer(time.Duration(ns))
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
		}
	}
}

func (m *module) hookFunc(f *wasmFile, spec hookSpec) hooks.Func {
	return func(hc *hooks.Context) error {
		if err := m.call(hc, f, spec); err != nil {
			return fmt.Errorf("wasm hook %s (%s): %w", f.name, hc.Event, err)
		}
		return nil
	}
}

// blocked is an operation a hook refused on purpose; the management API answers it with a 400.
type blocked struct{ msg string }

func (b *blocked) Error() string { return b.msg }
func (b *blocked) Unwrap() error { return content.ErrBadRequest }

func (m *module) call(hc *hooks.Context, f *wasmFile, spec hookSpec) error {
	parent := hc.Context
	if parent == nil {
		parent = context.Background()
	}
	req, err := buildRequest(hc)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	// Looking for a changed file (dev) and compiling it is not the hook's time.
	loadCtx, stop := context.WithTimeout(parent, compileTimeout)
	ref, release, err := f.get(loadCtx, m.rt, m.dev)
	stop()
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(parent, spec.timeout)
	defer cancel()
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-ctx.Done():
		return fmt.Errorf("no free slot within %s (modules.wasmhooks.concurrency is %d)", spec.timeout, cap(m.sem))
	}
	stdout := &capped{max: maxResponse}
	stderr := &capped{max: maxStderr, drop: true}
	cfg := wazero.NewModuleConfig().WithName("").WithArgs(f.name).
		WithStdin(bytes.NewReader(req)).WithStdout(stdout).WithStderr(stderr).
		WithSysWalltime().WithSysNanotime().WithNanosleep(sleeper(ctx)).WithRandSource(crand.Reader)
	mod, err := m.rt.InstantiateModule(ctx, ref.cm, cfg)
	if mod != nil {
		defer func() { _ = mod.Close(context.Background()) }()
	}
	m.logStderr(f, hc.Event, stderr)
	if err := runError(ctx, err, spec.timeout, stderr); err != nil {
		return err
	}
	if stdout.over {
		return fmt.Errorf("response is larger than %d bytes", maxResponse)
	}
	res, err := parseResponse(hc.Event, stdout.buf.Bytes())
	if err != nil {
		return err
	}
	return m.apply(hc, res)
}

// runError turns the outcome of instantiating (which runs _start) into the failure it means.
func runError(ctx context.Context, err error, timeout time.Duration, stderr *capped) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out after %s", timeout)
		}
		return ctx.Err()
	}
	var exit *sys.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() == 0 {
			return nil
		}
		return fmt.Errorf("exited with code %d%s", exit.ExitCode(), tail(stderr))
	}
	return fmt.Errorf("failed: %w%s", err, tail(stderr))
}

func tail(stderr *capped) string {
	s := strings.TrimSpace(stderr.buf.String())
	if s == "" {
		return ""
	}
	if i := strings.LastIndex(s[:max(0, len(s)-1)], "\n"); i >= 0 && len(s) > 300 {
		s = s[i+1:]
	}
	return " (stderr: " + truncate(s, 300) + ")"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (m *module) logStderr(f *wasmFile, ev hooks.Event, stderr *capped) {
	if s := strings.TrimSpace(stderr.buf.String()); s != "" {
		m.log.Info("wasm hook output", "hook", f.name, "event", string(ev), "stderr", s, "truncated", stderr.over)
	}
}

// apply carries out a validated response.
func (m *module) apply(hc *hooks.Context, res *response) error {
	if res.blocked {
		msg := res.block
		if msg == "" {
			msg = "blocked by the hook"
		}
		return &blocked{msg}
	}
	for _, fe := range res.errors {
		hc.Errors.Add(schema.Path(fe.Path), fe.Code, "%s", fe.Message)
	}
	if len(res.set) == 0 {
		return nil
	}
	return m.applySet(hc, res.set)
}

func (m *module) applySet(hc *hooks.Context, set map[string]any) error {
	if hc.Doc == nil {
		return errors.New("set: there is no document")
	}
	typ, ok := m.sc.Type(hc.Item.Type)
	if !ok {
		return fmt.Errorf("set: unknown content type %q", hc.Item.Type)
	}
	fields := make(map[string]bool, len(typ.Fields))
	for _, fd := range typ.Fields {
		fields[fd.Name] = true
	}
	vals := map[string]any{}
	for k, v := range set {
		if !fields[k] {
			return fmt.Errorf("set: %q is not a field of %s", k, typ.Name)
		}
		if v != nil {
			vals[k] = v
		}
	}
	m.sc.Normalize(typ, vals)
	for k := range set {
		if v, ok := vals[k]; ok {
			hc.Doc[k] = v
		} else {
			delete(hc.Doc, k)
		}
	}
	if hc.Event != hooks.BeforeSave {
		return nil // beforeValidate: the validation that follows checks the values
	}
	var bad []string
	for _, fe := range m.sc.Validate(typ, hc.Doc, schema.ModeDraft) {
		if _, touched := set[firstSegment(fe.Path)]; touched {
			bad = append(bad, fe.Error())
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("set: invalid values: %s", strings.Join(bad, "; "))
	}
	return nil
}

func firstSegment(path string) string {
	if i := strings.IndexAny(path, ".["); i >= 0 {
		return path[:i]
	}
	return path
}

// capped is a bounded output buffer. A stdout that overflows fails the write (the hook sees an I/O error);
// a stderr (drop) silently discards the excess.
type capped struct {
	buf  bytes.Buffer
	max  int
	drop bool
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.max - c.buf.Len()
	if len(p) <= room {
		return c.buf.Write(p)
	}
	c.over = true
	if c.drop {
		c.buf.Write(p[:room])
		return len(p), nil
	}
	return 0, errors.New("output limit exceeded")
}
