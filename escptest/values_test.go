package escptest_test

import (
	"context"
	"slices"
	"sync"
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

// The numbers of PROTOCOL.md are the language of the fake's scripts: a test outside the module has
// no constants for them (the type is the fake's own and exports none) and writes TLVType(12), the LED
// page. What the client asks for arrives as that number, and a reply scripted under it is the one
// the client reads.
func TestAScriptedReplyIsAddressedByTheNumberOfPROTOCOLMD(t *testing.T) {
	const ledType = escptest.TLVType(12)
	sw := escptest.Start(t)
	var mu sync.Mutex
	var asked []escptest.TLVType
	sw.Configure(func(f *escptest.Faults) {
		f.OnGet = func(types []escptest.TLVType) ([]escptest.Setting, int32, bool) {
			mu.Lock()
			defer mu.Unlock()
			asked = append(asked, types...)
			return []escptest.Setting{{Type: ledType, Value: []byte{1}}}, 0, false
		}
	})
	c := dial(sw)
	defer func() { _ = c.Close() }() // best effort at the end of the test
	on, err := c.LED(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Error("the LED reply scripted under TLVType(12) was not the one the client read")
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Contains(asked, ledType) {
		t.Errorf("the script was asked for %v, want it to include TLVType(12)", asked)
	}
}
