package wasmhooks

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
)

// Fixtures are Go programs under testdata, built for wasip1 once per test run.
var (
	fixtureDir   string
	sharedCache  = wazero.NewCompilationCache()
	modesBin     string // behaviour picked by the hook file's name
	constV1      string
	constV2      string
	buildFailure error
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wasmhooks-fixtures")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixtureDir = dir
	modesBin, constV1, constV2 = filepath.Join(dir, "modes.wasm"), filepath.Join(dir, "v1.wasm"), filepath.Join(dir, "v2.wasm")
	builds := [][]string{
		{"-o", modesBin, "./testdata/modes"},
		{"-o", constV1, "-ldflags", `-X main.reply={"set":{"notes":"v1"}}`, "./testdata/const"},
		{"-o", constV2, "-ldflags", `-X main.reply={"set":{"notes":"v2"}}`, "./testdata/const"},
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, args := range builds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command("go", append([]string{"build"}, args...)...)
			cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				mu.Lock()
				buildFailure = fmt.Errorf("building fixture %v: %v\n%s", args, err, out)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	code := 1
	if buildFailure != nil {
		fmt.Fprintln(os.Stderr, buildFailure)
	} else {
		code = m.Run()
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// hookDir returns a directory holding the modes fixture once per name (the name picks the behaviour).
func hookDir(t testing.TB, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		copyFile(t, modesBin, filepath.Join(dir, n+".wasm"))
	}
	return dir
}

func copyFile(t testing.TB, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func newLogger(w io.Writer) *slog.Logger { return slog.New(slog.NewTextHandler(w, nil)) }
