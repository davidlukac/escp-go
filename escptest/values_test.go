package escptest_test

import (
	"context"
	"testing"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
)

// The fake cannot name the client's types (see the package documentation), so what it takes and
// returns for a MAC address must stay assignable to escp.MAC without a conversion: a front end
// writes sw.SetMAC(mac) with an escp.MAC and mac := escptest.DefaultMAC() into one. These
// declarations stop compiling if the fake's type ever stops being an unnamed [6]byte.
var (
	_ escp.MAC = escptest.DefaultMAC()
	_ [6]byte  = escp.MAC{}
)

func TestAnEscpMACGoesIntoTheFakeAndTheClientReadsItBack(t *testing.T) {
	sw := escptest.Start(t)
	want := escp.MAC{0x02, 0, 0, 0, 0, 0x42}
	if want == escptest.DefaultMAC() {
		t.Fatal("the test needs an address other than the default")
	}
	sw.SetMAC(want) // no conversion
	c := dial(sw)
	defer func() { _ = c.Close() }() // best effort at the end of the test
	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.MAC != want {
		t.Errorf("the client read %v, want %v", info.MAC, want)
	}
}
