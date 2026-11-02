package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// lockedBuffer collects the output of a process that writes while the command reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// execTool is the go tool of this machine; command builds the processes it starts (tests replace it).
type execTool struct {
	command func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func (t execTool) module(ctx context.Context) (string, error) {
	out, err := t.command(ctx, "go", "list", "-m").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (t execTool) packages(ctx context.Context) ([]string, error) {
	out, err := t.command(ctx, "go", "list", "./...").Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// serve runs the pkgsite pinned in devtools/go.mod on the port, for the module in the current directory.
func (t execTool) serve(ctx context.Context, port int, out io.Writer) error {
	cmd := t.command(ctx, "go", "tool", "-modfile=devtools/go.mod", "pkgsite", "-http", "localhost:"+strconv.Itoa(port), ".")
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pkgsite: %w", err)
	}
	return nil
}
