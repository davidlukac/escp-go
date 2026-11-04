package escp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/davidlukac/escp-go/internal/wire"
)

var (
	macPin   = MAC{0xa8, 0x42, 0xa1, 0, 0, 0x01}
	macOther = MAC{0xc0, 0x3a, 0x55, 0, 0, 0x02}
	ipFrom   = net.IPv4(192, 0, 2, 50)
)

func TestAnUnpinnedIdentityLearnsTheFirstReplyAndHoldsToIt(t *testing.T) {
	var id identity
	if id.known() || !id.mac().IsZero() || !id.header().IsZero() {
		t.Fatal("a new identity knows nothing")
	}
	if !id.judge(macPin, ipFrom) || !id.known() || id.mac() != macPin || id.header() != macPin {
		t.Fatalf("the first contact is learned: known=%v mac=%v", id.known(), id.mac())
	}
	if id.judge(macOther, ipFrom) {
		t.Fatal("another device answered and was accepted")
	}
	wrong := id.wrongDevice()
	if wrong == nil || wrong.Want != macPin || wrong.Got != macOther || wrong.IP.String() != "192.0.2.50" {
		t.Fatalf("the refusal names both addresses and where the other one is: %+v", wrong)
	}
	if id.mac() != macPin || id.header() != macPin {
		t.Fatal("a refused device must not replace the identity")
	}
	id.forget()
	if id.wrongDevice() != nil {
		t.Fatal("forget clears the refused device")
	}
	if !id.judge(macPin, ipFrom) {
		t.Fatal("the switch itself is still accepted")
	}
}

func TestAPinnedIdentityRefusesEveryOtherMACFromTheFirstReply(t *testing.T) {
	id := identity{pin: macPin}
	if id.known() || id.mac() != macPin {
		t.Fatalf("a pinned identity is known by its pin before any reply: %v", id.mac())
	}
	if id.judge(macOther, ipFrom) {
		t.Fatal("the first reply came from another device and was accepted")
	}
	if !id.judge(macPin, ipFrom) || id.header() != macPin {
		t.Fatal("the pinned switch is accepted")
	}
}

// Reading the identity never waits for the lock of the client: it is safe beside the exchanges.
func TestTheIdentityIsSafeForConcurrentUse(t *testing.T) {
	id := identity{pin: macPin}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 200 {
				if i%2 == 0 {
					id.judge(macOther, ipFrom)
					id.forget()
				} else {
					_ = id.mac()
					_ = id.wrongDevice()
					_ = id.header()
				}
			}
		})
	}
	wg.Wait()
}

func TestPollUntilReportsWhetherTheProbeSaidYesInTime(t *testing.T) {
	t.Run("yes at once", func(t *testing.T) {
		retries := 0
		ok := pollUntil(t.Context(), time.Minute, time.Millisecond, func(context.Context) bool { return true }, func(context.Context) { retries++ })
		if !ok || retries != 0 {
			t.Fatalf("ok=%v retries=%d", ok, retries)
		}
	})
	t.Run("yes at the third ask", func(t *testing.T) {
		asks, retries := 0, 0
		ok := pollUntil(t.Context(), time.Minute, time.Millisecond, func(context.Context) bool { asks++; return asks == 3 }, func(context.Context) { retries++ })
		if !ok || asks != 3 || retries != 2 {
			t.Fatalf("ok=%v asks=%d retries=%d", ok, asks, retries)
		}
	})
	t.Run("never", func(t *testing.T) {
		asks := 0
		ok := pollUntil(t.Context(), 20*time.Millisecond, time.Millisecond, func(context.Context) bool { asks++; return false }, func(context.Context) {})
		if ok || asks == 0 {
			t.Fatalf("ok=%v asks=%d, want a no after at least one ask", ok, asks)
		}
	})
}

func TestNewSessionKey(t *testing.T) {
	key, err := newSessionKey(bytes.NewReader(bytes.Repeat([]byte{7}, 1024)))
	if err != nil || len(key) != wire.SessionKeyLen {
		t.Fatalf("got %q, %v", key, err)
	}
	for _, b := range key {
		if !strings.ContainsRune(sessionKeyAlphabet, rune(b)) {
			t.Fatalf("%q is outside the alphabet", key)
		}
	}
	cause := errors.New("no entropy")
	if _, err := newSessionKey(iotest.ErrReader(cause)); !errors.Is(err, cause) {
		t.Fatalf("got %v, want the reader's error", err)
	}
}
