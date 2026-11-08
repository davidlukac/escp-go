package escp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// a client that was refused with ErrSwitchInUse keeps sending to the switch (the key
// exchange replaces the switch's one session, which the other client is using).
func TestSeamARefusedClientStillTalksToTheSwitchOnEveryCall(t *testing.T) {
	f := escptest.Start(t)
	mac := MAC{0, 0, 0x5e, 0, 0x53, 0x42}
	f.SetMAC(mac)
	cfgAddr := Config{Host: "127.0.0.1", Username: "admin", Password: "secret", SwitchPort: f.UDPPort(), HostPort: escptest.FreeUDPPort(t), BroadcastAddr: escptest.Loopback(), Timeout: 5 * time.Second, Retries: NoRetries}
	loser, err := claimed(cfgAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { loser.Close() })
	cfgMAC := cfgAddr
	cfgMAC.Host, cfgMAC.MAC = "", mac
	owner, err := dial(context.Background(), net.DefaultResolver, cfgMAC, withClaim(), withLoopback(),
		withLocator(func(context.Context, MAC) (Info, error) {
			return Info{MAC: mac, IP: netip.MustParseAddr("127.0.0.1")}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	if _, err := loser.Info(context.Background()); !errors.Is(err, ErrSwitchInUse) {
		t.Fatalf("first contact: %v", err)
	}
	_, ownerErr := owner.Ports(context.Background())
	t.Logf("the other client (by MAC) when it finds the switch at the address the refused client still holds: %v", ownerErr)
	before := f.Received()
	_, err = loser.Ports(context.Background())
	t.Logf("second call of the refused client: err=%v; datagrams it sent to the switch: %d", err, f.Received()-before)
	if !errors.Is(err, ErrSwitchInUse) {
		t.Fatalf("want ErrSwitchInUse, got %v", err)
	}
	if f.Received() != before {
		t.Errorf("a client that is refused still sent %d datagram(s) to a switch another client holds (the key exchange replaces the session of the client that holds the switch)", f.Received()-before)
	}
}
