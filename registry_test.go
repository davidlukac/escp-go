package escp

import (
	"context"
	"errors"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// claimed dials a client that claims its switch, as a program's clients do.
func claimed(cfg Config) (*Client, error) {
	return dial(context.Background(), net.DefaultResolver, cfg, withClaim(), withLoopback())
}

func TestASecondClientOfTheSameSwitchIsRefused(t *testing.T) {
	port := escptest.FreeUDPPort(t)
	cfg := Config{Host: "127.0.0.1", SwitchPort: port}
	first, err := claimed(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	_, err = claimed(cfg)
	if !errors.Is(err, ErrSwitchInUse) || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("got %v, want ErrSwitchInUse naming the switch", err)
	}
}

func TestCloseReleasesTheSwitch(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t)}
	first, err := claimed(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := claimed(cfg)
	if err != nil {
		t.Fatalf("after Close the switch is free: %v", err)
	}
	t.Cleanup(func() { second.Close() })
	if err := first.Close(); err != nil { // idempotent, and it must not release the second client's claim
		t.Fatal(err)
	}
	if _, err := claimed(cfg); !errors.Is(err, ErrSwitchInUse) {
		t.Fatalf("a second Close of the first client released the second client's claim: %v", err)
	}
}

func TestSwitchesWithAnotherPortOrAnotherMACAreOtherSwitches(t *testing.T) {
	a, err := claimed(Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := claimed(Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t)})
	if err != nil {
		t.Fatalf("another port is another switch: %v", err)
	}
	c, err := claimed(Config{MAC: MAC{1, 2, 3, 4, 5, 6}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := claimed(Config{MAC: MAC{1, 2, 3, 4, 5, 7}})
	if err != nil {
		t.Fatalf("another MAC is another switch: %v", err)
	}
	for _, x := range []*Client{a, b, c, d} {
		x.Close()
	}
}

// The same switch under two names: by its MAC alone, and by its MAC and address.
func TestTheMACNamesTheSwitchWhateverTheAddress(t *testing.T) {
	mac := MAC{9, 9, 9, 9, 9, 1}
	first, err := claimed(Config{Host: "127.0.0.1", MAC: mac, SwitchPort: escptest.FreeUDPPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	if _, err := claimed(Config{MAC: mac}); !errors.Is(err, ErrSwitchInUse) {
		t.Fatalf("a client by MAC alone for a switch already claimed: %v", err)
	}
}

// A refused Dial claims nothing, not even the keys that were free.
func TestARefusedClientHoldsNothing(t *testing.T) {
	mac := MAC{9, 9, 9, 9, 9, 2}
	port := escptest.FreeUDPPort(t)
	first, err := claimed(Config{Host: "127.0.0.1", SwitchPort: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	if _, err := claimed(Config{Host: "127.0.0.1", SwitchPort: port, MAC: mac}); !errors.Is(err, ErrSwitchInUse) {
		t.Fatal(err)
	}
	other, err := claimed(Config{MAC: mac})
	if err != nil {
		t.Fatalf("the MAC of the refused client must be free: %v", err)
	}
	other.Close()
}

// A closed client that is used again claims its switch again, and fails if it was taken.
func TestAClosedClientUsedAgainNeedsItsSwitchBack(t *testing.T) {
	f := escptest.Start(t)
	cfg := Config{Host: "127.0.0.1", Username: "admin", Password: "secret", SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback(), Timeout: 5 * time.Second, Retries: NoRetries} // only a bound for a hang: the fake answers at once
	first, err := claimed(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := claimed(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Info(context.Background()); !errors.Is(err, ErrSwitchInUse) {
		t.Fatalf("got %v, want ErrSwitchInUse for the client whose switch was taken", err)
	}
	second.Close()
	if _, err := first.Info(context.Background()); err != nil {
		t.Fatalf("with the switch free again the first client works: %v", err)
	}
	first.Close()
}

// A client dropped without Close gives its claim up when it is collected.
func TestADroppedClientGivesUpItsSwitch(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t)}
	func() {
		if _, err := claimed(cfg); err != nil {
			t.Fatal(err)
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for {
		runtime.GC()
		c, err := claimed(cfg)
		if err == nil {
			c.Close()
			return
		}
		if !errors.Is(err, ErrSwitchInUse) || time.Now().After(deadline) {
			t.Fatalf("the claim of a dropped client was not given up: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConcurrentDialsOfOneSwitchLetExactlyOneIn(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t)}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var won []*Client
	for range 16 {
		wg.Go(func() {
			c, err := claimed(cfg)
			switch {
			case err == nil:
				mu.Lock()
				won = append(won, c)
				mu.Unlock()
			case !errors.Is(err, ErrSwitchInUse):
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(won) != 1 {
		t.Fatalf("%d clients got the switch, want 1", len(won))
	}
	won[0].Close()
}

// A switch that moves keeps its client's claim, now at the new address.
func TestAMovedSwitchIsClaimedAtItsNewAddress(t *testing.T) {
	c, err := claimed(Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.dest.IP = []byte{127, 0, 0, 2}
	c.reclaim()
	other, err := claimed(Config{Host: "127.0.0.2", SwitchPort: c.dest.Port})
	if !errors.Is(err, ErrSwitchInUse) {
		if err == nil {
			other.Close()
		}
		t.Fatalf("the new address is held by the moved client: %v", err)
	}
	old, err := claimed(Config{Host: "127.0.0.1", SwitchPort: c.dest.Port})
	if err != nil {
		t.Fatalf("the old address is free: %v", err)
	}
	old.Close()
}

// Another client holding the address a switch moves to is logged, not stolen from.
func TestAMoveOntoAClaimedAddressIsNotStolen(t *testing.T) {
	port := escptest.FreeUDPPort(t)
	holder, err := claimed(Config{Host: "127.0.0.2", SwitchPort: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { holder.Close() })
	c, err := claimed(Config{Host: "127.0.0.1", SwitchPort: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.dest.IP = []byte{127, 0, 0, 2}
	c.reclaim()
	if _, err := claimed(Config{Host: "127.0.0.1", SwitchPort: port}); !errors.Is(err, ErrSwitchInUse) {
		t.Fatalf("the moved client keeps the keys it had: %v", err)
	}
}
