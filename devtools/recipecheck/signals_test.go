package main

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestTheCheckListensToTheInterruptsItRestoresOn(t *testing.T) {
	want := []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
	if !slices.Equal(interrupts, want) {
		t.Errorf("interrupts %v, want %v", interrupts, want)
	}
}

// Each of the signals cancels the context and the process ends normally, as it must for its deferred
// restore to run: a helper process (this test binary) listens, says it is ready, and is sent the signal.
func TestEachInterruptCancelsTheContextOfAProcessThatListens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot send these signals to a process")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	signals := map[string]os.Signal{"INT": syscall.SIGINT, "TERM": syscall.SIGTERM, "HUP": syscall.SIGHUP}
	for name, sig := range signals {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(exe)
			cmd.Env = append(os.Environ(), "RECIPECHECK_HELPER=listen")
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(out)
			if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatalf("the helper said %q (%v)", line, err)
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			var rest strings.Builder
			for {
				line, err := reader.ReadString('\n')
				rest.WriteString(line)
				if err != nil {
					break
				}
			}
			if err := cmd.Wait(); err != nil || rest.String() != "canceled\n" {
				t.Errorf("wait: %v, output after the signal %q", err, rest.String())
			}
		})
	}
}
