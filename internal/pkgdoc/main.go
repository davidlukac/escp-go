// Command pkgdoc renders the package documentation the way pkg.go.dev does, from the working tree,
// and saves each page as HTML and as plain text under a directory (default build/pkgdoc,
// gitignored), so it can be read, diffed and reviewed without publishing anything. It runs the
// pkgsite version pinned in devtools/go.mod. It is a build tool of this repository, not part of
// the API.
//
//	go run ./internal/pkgdoc [OUT]    render and save
//	go run ./internal/pkgdoc --serve  serve on http://localhost:8089 until interrupted
//
// PKGDOC_PORT changes the port.
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Exit codes of the command.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const (
	defaultPort = 8089
	defaultOut  = "build/pkgdoc"
	// rootName is the file name of the page of the module's root package.
	rootName = "escp"
	// startupWait is how long pkgsite gets to answer its first request.
	startupWait = 2 * time.Minute
	// startupPoll is the pause between those requests.
	startupPoll = time.Second
	// dirMode and fileMode are the permissions of what the command writes.
	dirMode  = 0o750
	fileMode = 0o640
)

// goTool is what the command needs from the go tool and pkgsite; tests replace it.
type goTool interface {
	// module is the import path of the module in the current directory.
	module(ctx context.Context) (string, error)
	// packages lists the import paths of the module, internal ones included (pkg.go.dev lists them
	// under "Internal").
	packages(ctx context.Context) ([]string, error)
	// serve runs pkgsite on the port until ctx ends or it exits; its output goes to out.
	serve(ctx context.Context, port int, out io.Writer) error
}

// settings are what the command takes from its environment, so that tests can set them.
type settings struct {
	port         int
	client       *http.Client
	startupWait  time.Duration
	startupPoll  time.Duration
	interruptCtx func() (context.Context, context.CancelFunc)
}

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() {
	port, err := portFrom(os.Getenv("PKGDOC_PORT"))
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "pkgdoc:", err)
		os.Exit(exitUsage)
	}
	s := settings{
		port: port, client: http.DefaultClient, startupWait: startupWait, startupPoll: startupPoll,
		interruptCtx: func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt)
		},
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, execTool{command: exec.CommandContext}, s))
}

// portFrom reads PKGDOC_PORT: empty is the default port.
func portFrom(text string) (int, error) {
	if text == "" {
		return defaultPort, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("PKGDOC_PORT %q is not a port", text)
	}
	return n, nil
}

// run is the whole command: args are the command line without the program name.
func run(args []string, stdout, stderr io.Writer, tool goTool, s settings) int {
	switch {
	case len(args) == 1 && args[0] == "--serve":
		ctx, stop := s.interruptCtx()
		defer stop()
		if err := tool.serve(ctx, s.port, stdout); err != nil && ctx.Err() == nil {
			_, _ = fmt.Fprintf(stderr, "pkgdoc: pkgsite: %v\n", err)
			return exitFailure
		}
		return exitOK
	case len(args) > 1 || (len(args) == 1 && strings.HasPrefix(args[0], "-")):
		_, _ = fmt.Fprintln(stderr, "usage: pkgdoc [OUT] | pkgdoc --serve")
		return exitUsage
	}
	out := defaultOut
	if len(args) == 1 {
		out = args[0]
	}
	if err := render(out, stdout, tool, s); err != nil {
		_, _ = fmt.Fprintf(stderr, "pkgdoc: %v\n", err)
		return exitFailure
	}
	return exitOK
}

// render starts pkgsite, fetches the page of every package and saves it under out.
func render(out string, stdout io.Writer, tool goTool, s settings) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	module, err := tool.module(ctx)
	if err != nil {
		return fmt.Errorf("reading the module path: %w", err)
	}
	pkgs, err := tool.packages(ctx)
	if err != nil {
		return fmt.Errorf("listing the packages: %w", err)
	}
	//nolint:gosec // the directory the operator names on the command line, rebuilt on every run
	if err := errors.Join(os.RemoveAll(out), os.MkdirAll(out, dirMode)); err != nil {
		return err
	}
	var log lockedBuffer
	site := &runningSite{done: make(chan struct{})}
	go func() {
		site.err = tool.serve(ctx, s.port, &log)
		close(site.done)
	}()
	defer func() { cancel(); <-site.done }() // pkgsite stops before the command returns
	base := "http://localhost:" + strconv.Itoa(s.port) + "/"
	if err := waitUntilServing(ctx, s, base+module, site, &log); err != nil {
		return err
	}
	for _, pkg := range pkgs {
		page, err := fetch(ctx, s.client, base+pkg)
		if err != nil {
			return fmt.Errorf("fetching %s: %w", pkg, err)
		}
		name := pageName(module, pkg)
		for ext, data := range map[string][]byte{".html": page, ".txt": []byte(htmlToText(string(page)))} {
			//nolint:gosec // inside the directory the operator named; the name comes from this module's package paths
			if err := os.WriteFile(filepath.Join(out, name+ext), data, fileMode); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(stdout, "saved %s/%s.html and .txt\n", out, name)
	}
	return nil
}

// pageName is the file name (without extension) of a package's page: its path inside the module with
// the slashes made underscores, and rootName for the root package.
func pageName(module, pkg string) string {
	name := strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(pkg, module), "/"), "/", "_")
	if name == "" {
		return rootName
	}
	return name
}

// runningSite is the pkgsite process the command started: done closes when it has exited, and err
// (valid after that) says how.
type runningSite struct {
	done chan struct{}
	err  error
}

// waitUntilServing polls url until pkgsite answers it, gives up after s.startupWait, and reports
// pkgsite's own output when it exits first.
func waitUntilServing(ctx context.Context, s settings, url string, site *runningSite, log *lockedBuffer) error {
	deadline := time.Now().Add(s.startupWait)
	for {
		if _, err := fetch(ctx, s.client, url); err == nil {
			return nil
		}
		select {
		case <-site.done:
			return fmt.Errorf("pkgsite exited: %w\n%s", cmp.Or(site.err, errExitedCleanly), log.String())
		case <-time.After(s.startupPoll):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pkgsite did not start:\n%s", log.String())
		}
	}
}

// errExitedCleanly is the cause given for a pkgsite that exited without an error before it served.
var errExitedCleanly = errors.New("exit status 0")

// errStatus is the cause of a page that pkgsite answers with an error status.
var errStatus = errors.New("unexpected status")

// fetch returns the body of url, or an error for a failed request or a status other than 200.
func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() // a read-only response: nothing to lose on close
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s", errStatus, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
