package escp

import (
	"context"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// libraryGoroutines returns the stacks of the goroutines that are running code of this module and
// are not a test: what a client, a discovery or a gate left running. The fake switch is closed
// before this looks, so its serving goroutine is gone too.
func libraryGoroutines(t *testing.T) []string {
	t.Helper()
	var profile strings.Builder
	if err := pprof.Lookup("goroutine").WriteTo(&profile, 1); err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, stack := range strings.Split(profile.String(), "\n\n") {
		switch {
		case !strings.Contains(stack, "github.com/davidlukac/escp-go"):
		case strings.Contains(stack, "testing.tRunner"), strings.Contains(stack, "testing.(*T).Run"), strings.Contains(stack, "testing.runTests"):
			// a test, which is running this check, or a parent waiting for it
		default:
			left = append(left, stack)
		}
	}
	return left
}

// soakCycles is how many times the leak test dials, uses and closes a client: a few for every run of
// the suite, more when ESCP_SOAK says so (the weekly soak job sets it).
func soakCycles(t *testing.T) int {
	t.Helper()
	cycles := 20
	if v := os.Getenv("ESCP_SOAK"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("ESCP_SOAK=%q is not a positive number", v)
		}
		cycles = n
	}
	return cycles
}

// A client that is used and closed leaves nothing running: not after a read, not after a write whose
// context ends, not against a switch that never answers and a call that is canceled. The goroutines
// are looked at by what they run (code of this module), not counted, so other tests cannot disturb it.
func TestUsedAndClosedClientsLeaveNoGoroutinesBehind(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	dial := func() *Client {
		return newBoundClient(t, Config{Host: "127.0.0.1", Username: escptest.Username, Password: escptest.Password,
			Timeout: 200 * time.Millisecond, Retries: NoRetries, SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()})
	}
	ctx := context.Background()
	cycles := soakCycles(t)
	for i := range cycles {
		c := dial()
		if _, err := c.Ports(ctx); err != nil {
			t.Fatal(err)
		}
		short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		_ = c.SetLED(short, i%2 == 0) // may finish or run out of time: either is fine here
		cancel()
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	f.Mute() // a switch that is gone: every call waits and is canceled
	for range cycles {
		c := dial()
		canceled, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		_, _ = c.Ports(canceled)
		cancel()
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// a goroutine that is on its way out may take a moment: look until none is left, or give up
	var left []string
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if left = libraryGoroutines(t); len(left) == 0 {
			return
		}
	}
	t.Fatalf("%d goroutine(s) of the library are still running after %d cycles:\n%s", len(left), cycles, strings.Join(left, "\n\n"))
}

// The check itself sees a goroutine that runs code of this module and is not a test, and sees it go.
func TestTheLeakCheckSeesAStrayGoroutine(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-release
	}()
	if got := libraryGoroutines(t); len(got) != 1 {
		t.Fatalf("a goroutine of this module is blocked, the check found %d:\n%s", len(got), strings.Join(got, "\n\n"))
	}
	close(release)
	<-done
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if len(libraryGoroutines(t)) == 0 {
			return
		}
	}
	t.Fatal("the goroutine ended, the check still sees it")
}
