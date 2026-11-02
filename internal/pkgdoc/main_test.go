package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTool stands in for the go tool and pkgsite.
type fakeTool struct {
	moduleName string
	moduleErr  error
	pkgs       []string
	pkgsErr    error
	serveFn    func(ctx context.Context, port int, out io.Writer) error
}

func (f fakeTool) module(context.Context) (string, error)     { return f.moduleName, f.moduleErr }
func (f fakeTool) packages(context.Context) ([]string, error) { return f.pkgs, f.pkgsErr }

func (f fakeTool) serve(ctx context.Context, port int, out io.Writer) error {
	if f.serveFn == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.serveFn(ctx, port, out)
}

// pagesServer serves the pages by path, with an HTML body naming the path; unknown paths are 404s.
func pagesServer(t *testing.T, pages map[string]string) (port int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return portOf(t, srv.URL)
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func quickSettings(port int) settings {
	return settings{port: port, client: http.DefaultClient, startupWait: 2 * time.Second, startupPoll: 5 * time.Millisecond}
}

func runWith(t *testing.T, tool goTool, s settings, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(args, &o, &e, tool, s)
	return code, o.String(), e.String()
}

func TestEachPackageIsSavedAsHTMLAndAsTextUnderItsNameInTheModule(t *testing.T) {
	port := pagesServer(t, map[string]string{
		"/example.org/m":      "<h1>Package m</h1><p>root</p>",
		"/example.org/m/a/b":  "<p>nested &amp; internal</p>",
		"/example.org/m/zero": "<p>zero</p>",
	})
	out := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "stale.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := fakeTool{moduleName: "example.org/m", pkgs: []string{"example.org/m", "example.org/m/a/b", "example.org/m/zero"}}
	code, stdout, errOut := runWith(t, tool, quickSettings(port), out)
	if code != exitOK || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	for file, want := range map[string]string{
		"escp.html": "<h1>Package m</h1><p>root</p>", "escp.txt": "# Package m\n\nroot\n",
		"a_b.html": "<p>nested &amp; internal</p>", "a_b.txt": "nested & internal\n",
		"zero.txt": "zero\n",
	} {
		got, err := os.ReadFile(filepath.Join(out, file))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", file, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "stale.txt")); err == nil {
		t.Error("a file of an earlier run was left in the directory")
	}
	for _, want := range []string{"saved " + out + "/escp.html and .txt\n", "saved " + out + "/a_b.html and .txt\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestTheDefaultDirectoryIsBuildPkgdoc(t *testing.T) {
	port := pagesServer(t, map[string]string{"/m": "<p>x</p>"})
	t.Chdir(t.TempDir())
	tool := fakeTool{moduleName: "m", pkgs: []string{"m"}}
	if code, _, errOut := runWith(t, tool, quickSettings(port)); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join("build", "pkgdoc", "escp.txt")); err != nil {
		t.Error(err)
	}
}

func TestPkgsiteIsGivenTheTimeItNeedsToStart(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if asked.Add(1) <= 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "<p>up</p>")
	}))
	t.Cleanup(srv.Close)
	tool := fakeTool{moduleName: "m", pkgs: []string{"m"}}
	code, _, errOut := runWith(t, tool, quickSettings(portOf(t, srv.URL)), filepath.Join(t.TempDir(), "o"))
	if code != exitOK || asked.Load() < 4 {
		t.Fatalf("code %d, stderr %q, %d requests; want it to wait out the first three", code, errOut, asked.Load())
	}
}

func TestPkgsiteThatNeverAnswersIsReportedWithItsOutput(t *testing.T) {
	tool := fakeTool{moduleName: "m", pkgs: []string{"m"}, serveFn: func(ctx context.Context, _ int, out io.Writer) error {
		_, _ = io.WriteString(out, "listening on nothing\n")
		<-ctx.Done()
		return ctx.Err()
	}}
	s := quickSettings(freePort(t))
	s.startupWait = 60 * time.Millisecond
	code, _, errOut := runWith(t, tool, s, filepath.Join(t.TempDir(), "o"))
	if code != exitFailure || !strings.Contains(errOut, "pkgsite did not start:\nlistening on nothing") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestPkgsiteThatExitsWithoutAnErrorBeforeServingIsReportedToo(t *testing.T) {
	tool := fakeTool{moduleName: "m", pkgs: []string{"m"}, serveFn: func(context.Context, int, io.Writer) error { return nil }}
	code, _, errOut := runWith(t, tool, quickSettings(freePort(t)), filepath.Join(t.TempDir(), "o"))
	if code != exitFailure || !strings.Contains(errOut, "pkgsite exited: exit status 0") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestPkgsiteThatExitsEarlyIsReportedWithItsOutput(t *testing.T) {
	tool := fakeTool{moduleName: "m", pkgs: []string{"m"}, serveFn: func(_ context.Context, _ int, out io.Writer) error {
		_, _ = io.WriteString(out, "address already in use\n")
		return errors.New("exit status 1")
	}}
	code, _, errOut := runWith(t, tool, quickSettings(freePort(t)), filepath.Join(t.TempDir(), "o"))
	if code != exitFailure || !strings.Contains(errOut, "pkgsite exited: exit status 1\naddress already in use") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }() // released at once: the test wants a port nobody listens on
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("%T is not a TCP address", l.Addr())
	}
	return addr.Port
}

func TestAPageThatPkgsiteRefusesFailsTheRunNamingThePackage(t *testing.T) {
	port := pagesServer(t, map[string]string{"/m": "<p>x</p>"})
	tool := fakeTool{moduleName: "m", pkgs: []string{"m", "m/missing"}}
	code, _, errOut := runWith(t, tool, quickSettings(port), filepath.Join(t.TempDir(), "o"))
	if code != exitFailure || !strings.Contains(errOut, "fetching m/missing: unexpected status: 404 Not Found") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestWhatTheGoToolCannotSayStopsTheRunBeforeAnythingIsWritten(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o")
	for name, tool := range map[string]fakeTool{
		"module":   {moduleErr: errors.New("no go.mod")},
		"packages": {moduleName: "m", pkgsErr: errors.New("build failed")},
	} {
		code, _, errOut := runWith(t, tool, quickSettings(1), out)
		if code != exitFailure || !strings.Contains(errOut, "no go.mod") && !strings.Contains(errOut, "build failed") {
			t.Errorf("%s: code %d, stderr %q", name, code, errOut)
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: the output directory was created", name)
		}
	}
}

func TestAnOutputDirectoryThatCannotBeMadeOrWrittenFailsTheRun(t *testing.T) {
	port := pagesServer(t, map[string]string{"/m": "<p>x</p>", "/" + strings.Repeat("n", 300): "<p>y</p>"})
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tool := fakeTool{moduleName: "m", pkgs: []string{"m"}}
	if code, _, errOut := runWith(t, tool, quickSettings(port), filepath.Join(file, "sub")); code != exitFailure || errOut == "" {
		t.Errorf("a directory under a file: code %d, stderr %q", code, errOut)
	}
	long := fakeTool{moduleName: "m", pkgs: []string{"m", strings.Repeat("n", 300)}}
	if code, _, errOut := runWith(t, long, quickSettings(port), filepath.Join(t.TempDir(), "o")); code != exitFailure || errOut == "" {
		t.Errorf("a file name that is too long: code %d, stderr %q", code, errOut)
	}
}

func TestServeRunsPkgsiteOnThePortUntilTheInterrupt(t *testing.T) {
	var gotPort int
	ctx, cancel := context.WithCancel(context.Background())
	tool := fakeTool{serveFn: func(ctx context.Context, port int, _ io.Writer) error {
		gotPort = port
		cancel() // the operator interrupts
		<-ctx.Done()
		return ctx.Err()
	}}
	s := quickSettings(8123)
	s.interruptCtx = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	if code, _, errOut := runWith(t, tool, s, "--serve"); code != exitOK || errOut != "" || gotPort != 8123 {
		t.Fatalf("code %d, stderr %q, port %d", code, errOut, gotPort)
	}
}

func TestServeReportsPkgsiteThatFailsOnItsOwn(t *testing.T) {
	tool := fakeTool{serveFn: func(context.Context, int, io.Writer) error { return errors.New("exit status 1") }}
	s := quickSettings(8123)
	s.interruptCtx = func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }
	code, _, errOut := runWith(t, tool, s, "--serve")
	if code != exitFailure || !strings.Contains(errOut, "pkgsite: exit status 1") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestAnythingButAnOutputDirectoryOrServeIsAUsageError(t *testing.T) {
	for _, args := range [][]string{{"a", "b"}, {"--unknown"}, {"-x"}, {"--serve", "extra"}} {
		code, _, errOut := runWith(t, fakeTool{}, quickSettings(1), args...)
		if code != exitUsage || !strings.Contains(errOut, "usage: pkgdoc") {
			t.Errorf("%v: code %d, stderr %q", args, code, errOut)
		}
	}
}

func TestThePortComesFromTheEnvironmentOrIsTheDefault(t *testing.T) {
	for in, want := range map[string]int{"": 8089, "9000": 9000, "1": 1, "65535": 65535} {
		if got, err := portFrom(in); err != nil || got != want {
			t.Errorf("portFrom(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"x", "0", "65536", "-1", "80.5"} {
		if _, err := portFrom(in); err == nil {
			t.Errorf("portFrom(%q) was accepted", in)
		}
	}
}

func TestAPackageNameIsItsPathInTheModuleWithUnderscores(t *testing.T) {
	for pkg, want := range map[string]string{
		"example.org/m": "escp", "example.org/m/safety": "safety", "example.org/m/internal/wire": "internal_wire",
	} {
		if got := pageName("example.org/m", pkg); got != want {
			t.Errorf("pageName(%q) = %q, want %q", pkg, got, want)
		}
	}
}

// helperEnv makes the test binary act as the fake "go" command instead of running tests.
const helperEnv = "PKGDOC_TEST_HELPER"

// TestMain lets the test binary stand in for the go tool: execTool starts it with the arguments it
// would give to go, and the helper prints what a go command would, or fails.
func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(helperProcess(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func helperProcess(mode string, args []string) int {
	switch mode {
	case "echo":
		fmt.Println(strings.Join(args, " "))
	case "module":
		fmt.Println("example.org/m")
	case "packages":
		fmt.Println("example.org/m\nexample.org/m/safety")
	case "fail":
		fmt.Fprintln(os.Stderr, "boom")
		return 3
	}
	return 0
}

func helperCommand(mode string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{name}, args...)...)
		cmd.Env = append(os.Environ(), helperEnv+"="+mode)
		return cmd
	}
}

func TestTheGoToolNamesTheModuleAndItsPackages(t *testing.T) {
	ctx := context.Background()
	if got, err := (execTool{command: helperCommand("module")}).module(ctx); err != nil || got != "example.org/m" {
		t.Errorf("module = %q, %v", got, err)
	}
	pkgs, err := execTool{command: helperCommand("packages")}.packages(ctx)
	if err != nil || !reflect.DeepEqual(pkgs, []string{"example.org/m", "example.org/m/safety"}) {
		t.Errorf("packages = %v, %v", pkgs, err)
	}
}

func TestPkgsiteIsStartedFromTheModulePinnedInDevtoolsOnTheGivenPort(t *testing.T) {
	var out bytes.Buffer
	if err := (execTool{command: helperCommand("echo")}).serve(context.Background(), 8089, &out); err != nil {
		t.Fatal(err)
	}
	want := "go tool -modfile=devtools/go.mod pkgsite -http localhost:8089 .\n"
	if out.String() != want {
		t.Errorf("started as %q, want %q", out.String(), want)
	}
}

func TestAFailingGoToolIsReported(t *testing.T) {
	ctx := context.Background()
	tool := execTool{command: helperCommand("fail")}
	if _, err := tool.module(ctx); err == nil {
		t.Error("a failing go list -m was not reported")
	}
	if _, err := tool.packages(ctx); err == nil {
		t.Error("a failing go list was not reported")
	}
	var out bytes.Buffer
	if err := tool.serve(ctx, 1, &out); err == nil || !strings.Contains(out.String(), "boom") {
		t.Errorf("a failing pkgsite: err %v, output %q; want the error and the output", err, out.String())
	}
}

func TestTheLockedBufferCollectsWhatTwoWritersSend(t *testing.T) {
	var b lockedBuffer
	done := make(chan struct{})
	go func() {
		for range 100 {
			_, _ = b.Write([]byte("a"))
		}
		close(done)
	}()
	for range 100 {
		_, _ = b.Write([]byte("b"))
	}
	<-done
	if got := b.String(); len(got) != 200 {
		t.Errorf("collected %d bytes, want 200", len(got))
	}
}

func TestFetchRefusesAnAddressThatIsNoURL(t *testing.T) {
	if _, err := fetch(context.Background(), http.DefaultClient, "http://bad host/"); err == nil {
		t.Fatal("an address with a space in the host was fetched")
	}
}
