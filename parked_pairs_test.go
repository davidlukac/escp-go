package escp

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// Every method that reads a setting and writes it back (the callers of beginModify) has a test
// that parks the call between its read and its write, runs a second call that touches the same
// request or state, releases the first and asserts the final state: both changes are there, and no
// write carried a value that was stale by then (DC-L03). The catalog's first test for the class
// parks two calls of one method (TestReadModifyWriteCallsDoNotUndoEachOther); the rows below
// cover the pairs that the incident of #141 and the guards of #183 were about.

// parkedPairWanted is an address other than the fake's static one and its lease: the three
// differ in the mask and the gateway (the fake answers only from one IP), so a write that carries
// the wrong one can be told from the right one.
var parkedPairWanted = cfgOf("127.0.0.1", "255.255.255.128", "127.0.0.3")

const parkedPairName model.Hostname = "sw-parked"

// parkedPairPorts are the two ports of the port rows: the first call changes one, the second the other.
const (
	parkedPairPortA model.PortID = 3
	parkedPairPortB model.PortID = 4
)

// parkedPair is one row: the first call is held in the write gate after its read, the second call
// runs, then the first is released.
type parkedPair struct {
	name   string
	first  model.Method // the method of the held call; it is the key the completeness test looks for
	second model.Method // the partner: it touches the same wire request or the same state
	// callFirst and callSecond are the two calls.
	callFirst, callSecond func(ctx context.Context, c *Client) error
	// wantFirst and wantSecond are what the calls return when they run one after the other (nil
	// is success); the second may be refused because the first has just changed what it checks.
	wantFirst, wantSecond error
	// check asserts the final state, read through the client, and the writes the fake received.
	check func(t *testing.T, c *Client, f *escptest.Switch)
}

func callHostname(name model.Hostname) func(context.Context, *Client) error {
	return func(ctx context.Context, c *Client) error { return c.SetHostname(ctx, name) }
}

func callIPConfig(cfg model.IPConfig) func(context.Context, *Client) error {
	return func(ctx context.Context, c *Client) error { return c.SetIPConfig(ctx, cfg) }
}

func callEnableDHCP(ctx context.Context, c *Client) error { return c.EnableDHCP(ctx) }

func callBandwidth(limit model.PortBandwidth) func(context.Context, *Client) error {
	return func(ctx context.Context, c *Client) error { return c.SetBandwidth(ctx, limit) }
}

func callStorm(cfg model.StormConfig) func(context.Context, *Client) error {
	return func(ctx context.Context, c *Client) error { return c.SetStormControl(ctx, cfg) }
}

func callPriority(port model.PortID) func(context.Context, *Client) error {
	return func(ctx context.Context, c *Client) error {
		return c.SetPortPriorities(ctx, []model.PortPriority{{Port: port, Priority: model.PriorityHighest}})
	}
}

// parkedPairInfo reads the System page the way a caller does, and fails the test when it cannot.
func parkedPairInfo(t *testing.T, c *Client) model.Info {
	t.Helper()
	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatalf("reading the switch after both calls: %v", err)
	}
	return info
}

// wantStatic asserts the switch is static at the wanted address.
func wantStatic(t *testing.T, info model.Info) {
	t.Helper()
	if info.DHCP || info.Netmask != parkedPairWanted.Netmask || info.Gateway != parkedPairWanted.Gateway {
		t.Errorf("the switch should hold the static address that SetIPConfig wrote (dhcp off, mask %s, gateway %s); it reports dhcp=%v mask=%s gateway=%s",
			parkedPairWanted.Netmask, parkedPairWanted.Gateway, info.DHCP, info.Netmask, info.Gateway)
	}
}

// wantLeasing asserts the switch leases its address.
func wantLeasing(t *testing.T, info model.Info) {
	t.Helper()
	if !info.DHCP {
		t.Error("the switch should lease its address (EnableDHCP was applied); the other call's write switched the flag off again")
	}
}

func wantName(t *testing.T, info model.Info) {
	t.Helper()
	if info.Hostname != parkedPairName {
		t.Errorf("the device description should be %q (SetHostname was applied); it is %q", parkedPairName, info.Hostname)
	}
}

// parkedPairs is the table; every caller of beginModify is the first of at least one row
// (TestEveryReadModifyWriteMethodHasAParkedPairTest).
var parkedPairs = []parkedPair{
	{
		name:  "SetHostname held, SetIPConfig runs: the incident of #141",
		first: model.MethodSetHostname, second: model.MethodSetIPConfig,
		callFirst: callHostname(parkedPairName), callSecond: callIPConfig(parkedPairWanted),
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			info := parkedPairInfo(t, c)
			wantName(t, info)
			wantStatic(t, info)
		},
	},
	{
		name:  "SetIPConfig held, SetHostname runs",
		first: model.MethodSetIPConfig, second: model.MethodSetHostname,
		callFirst: callIPConfig(parkedPairWanted), callSecond: callHostname(parkedPairName),
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			info := parkedPairInfo(t, c)
			wantName(t, info)
			wantStatic(t, info)
		},
	},
	{
		name:  "EnableDHCP held, SetHostname runs",
		first: model.MethodEnableDHCP, second: model.MethodSetHostname,
		callFirst: callEnableDHCP, callSecond: callHostname(parkedPairName),
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			info := parkedPairInfo(t, c)
			wantName(t, info)
			wantLeasing(t, info)
		},
	},
	{
		name:  "SetHostname held, EnableDHCP runs",
		first: model.MethodSetHostname, second: model.MethodEnableDHCP,
		callFirst: callHostname(parkedPairName), callSecond: callEnableDHCP,
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			info := parkedPairInfo(t, c)
			wantName(t, info)
			wantLeasing(t, info)
		},
	},
	{
		name:  "EnableDHCP held, SetIPConfig runs: the flag is switched off after it was on",
		first: model.MethodEnableDHCP, second: model.MethodSetIPConfig,
		callFirst: callEnableDHCP, callSecond: callIPConfig(parkedPairWanted),
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			wantStatic(t, parkedPairInfo(t, c))
		},
	},
	{
		name:  "SetIPConfig held, EnableDHCP runs: the lease request must carry the new address",
		first: model.MethodSetIPConfig, second: model.MethodEnableDHCP,
		callFirst: callIPConfig(parkedPairWanted), callSecond: callEnableDHCP,
		check: func(t *testing.T, c *Client, f *escptest.Switch) {
			t.Helper()
			wantLeasing(t, parkedPairInfo(t, c))
			sets := f.AllSets()
			if len(sets) != 2 {
				t.Fatalf("want one write per call, got %d", len(sets))
			}
			wantRequestAddress(t, sets[1], parkedPairWanted)
		},
	},
	{
		name:  "SetBandwidth held, SetStormControl runs: the ingress limit and storm control exclude each other",
		first: model.MethodSetBandwidth, second: model.MethodSetStormControl,
		callFirst:  callBandwidth(model.PortBandwidth{Port: parkedPairPortA, Ingress: model.RateStep, Egress: model.RateStep}),
		callSecond: callStorm(model.StormConfig{Port: parkedPairPortA, Broadcast: true, Rate: model.RateStep}),
		wantSecond: ErrInvalid,
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			limits, rows := portLimits(t, c, parkedPairPortA)
			if limits.Ingress != model.RateStep || rows.Enabled {
				t.Errorf("the ingress limit was applied first, so storm control must stay off: ingress=%s storm=%+v", limits.Ingress, rows)
			}
		},
	},
	{
		name:  "SetStormControl held, SetBandwidth runs",
		first: model.MethodSetStormControl, second: model.MethodSetBandwidth,
		callFirst:  callStorm(model.StormConfig{Port: parkedPairPortA, Broadcast: true, Rate: model.RateStep}),
		callSecond: callBandwidth(model.PortBandwidth{Port: parkedPairPortA, Ingress: model.RateStep, Egress: model.RateStep}),
		wantSecond: ErrInvalid,
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			limits, rows := portLimits(t, c, parkedPairPortA)
			if limits.Ingress != 0 || !rows.Enabled {
				t.Errorf("storm control was applied first, so the ingress limit must stay off: ingress=%s storm=%+v", limits.Ingress, rows)
			}
		},
	},
	{
		name:  "SetPortPriorities held, SetPortPriorities runs for another port",
		first: model.MethodSetPortPriorities, second: model.MethodSetPortPriorities,
		callFirst: callPriority(parkedPairPortA), callSecond: callPriority(parkedPairPortB),
		check: func(t *testing.T, c *Client, _ *escptest.Switch) {
			t.Helper()
			got, err := c.QoS(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range got.Priorities {
				if (p.Port == parkedPairPortA || p.Port == parkedPairPortB) && p.Priority != model.PriorityHighest {
					t.Errorf("port %d has priority %d: the other call undid its change", p.Port, p.Priority)
				}
			}
		},
	},
}

// wantRequestAddress asserts a System-page write carries the given address, mask and gateway.
func wantRequestAddress(t *testing.T, set []escptest.Setting, want model.IPConfig) {
	t.Helper()
	got := map[escptest.TLVType][]byte{}
	for _, s := range set {
		got[s.Type] = s.Value
	}
	for typ, addr := range map[wire.TLVType][]byte{
		wire.TLVIP: want.IP.AsSlice(), wire.TLVNetmask: want.Netmask.AsSlice(), wire.TLVGateway: want.Gateway.AsSlice(),
	} {
		if v := got[escptest.TLVType(typ)]; string(v) != string(addr) {
			t.Errorf("the write carried %v for TLV %d, a stale value: it should carry what the other call wrote, %v", v, typ, addr)
		}
	}
}

// portLimits reads the ingress limit and the storm control row of one port.
func portLimits(t *testing.T, c *Client, port model.PortID) (model.PortBandwidth, model.PortStorm) {
	t.Helper()
	var limits model.PortBandwidth
	var rows model.PortStorm
	all, err := c.Bandwidth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range all {
		if b.Port == port {
			limits = b
		}
	}
	storms, err := c.StormControl(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range storms {
		if s.Port == port {
			rows = s
		}
	}
	return limits, rows
}

// pairGate holds the first write of one method after the call has read what it writes back. The
// other calls pass. It is the seam the write gate offers: the gate runs after the method's reads
// and before its write.
type pairGate struct {
	method  model.Method
	once    sync.Once
	parked  chan struct{} // closed when the first write of the method is held
	release chan struct{} // closed by the test to let it go
}

func newPairGate(method model.Method) *pairGate {
	return &pairGate{method: method, parked: make(chan struct{}), release: make(chan struct{})}
}

func (g *pairGate) check(ctx context.Context, _ Reader, op Operation) error {
	if op.Name != g.method {
		return nil
	}
	held := false
	g.once.Do(func() { held = true })
	if !held {
		return nil
	}
	close(g.parked)
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// queuedForModify reports whether a call is waiting for c.modify: acquire starts a goroutine that
// blocks in Lock while another call holds the mutex, and nothing else in the package does. It is
// how the test knows, without a pause, that the second call has reached the lock and cannot run
// before the first is released.
func queuedForModify() bool {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, ".acquire.func1") && strings.Contains(g, "sync.(*Mutex).Lock") {
			return true
		}
	}
	return false
}

// awaitSecond returns once the second call has finished (finished is true, with its result) or is
// queued behind the first (finished is false). It yields to the scheduler between looks, and
// needs no pause: one of the two always comes.
func awaitSecond(second <-chan error) (err error, finished bool) {
	for {
		select {
		case err := <-second:
			return err, true
		default:
		}
		if queuedForModify() {
			return nil, false
		}
		runtime.Gosched()
	}
}

// parkedPairClient is a client of the fake with the held write gate; it pins the MAC and looks
// the switch up by asking the fake itself, which EnableDHCP needs.
func parkedPairClient(t *testing.T, f *escptest.Switch, gate *pairGate) *Client {
	t.Helper()
	askTheFake := func(c *Client) {
		c.locator = func(ctx context.Context, _ MAC) (Info, error) { return c.discoverOnce(ctx) }
	}
	c := newBoundClient(t, Config{Host: "127.0.0.1", MAC: escptest.DefaultMAC(), Username: "admin", Password: "secret",
		Timeout: 2 * time.Second, SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback(), WriteGate: gate.check},
		withLoopback(), askTheFake)
	t.Cleanup(func() { c.Close() })
	return c
}

// wantResult asserts a call returned the error its row expects (nil for success).
func wantResult(t *testing.T, which string, got, want error) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s call: %v", which, got)
	case want != nil && !errors.Is(got, want):
		t.Errorf("%s call: got %v, want %v (it runs after the other call and must see its change)", which, got, want)
	}
}

// The part of Client's "safe for concurrent use" that is about state: two calls that read a
// setting and write it back do not undo each other.
//
// guarantee: Client
func TestReadModifyWritePairsKeepBothChanges(t *testing.T) {
	for _, tc := range parkedPairs {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			f.EnableModel(nil)
			gate := newPairGate(tc.first)
			c := parkedPairClient(t, f, gate)
			ctx := context.Background()

			first := make(chan error, 1)
			go func() { first <- tc.callFirst(ctx, c) }()
			select {
			case <-gate.parked:
			case err := <-first:
				t.Fatalf("the first call ended before its write: %v", err)
			}
			second := make(chan error, 1)
			go func() { second <- tc.callSecond(ctx, c) }()
			secondErr, finished := awaitSecond(second)
			if finished {
				// Without the lock the second call completes here, between the read and the
				// write of the first, and the first then writes back what it read before.
				t.Errorf("%s ran to its end while %s was held between its read and its write", tc.second, tc.first)
			}
			close(gate.release)
			wantResult(t, "first", <-first, tc.wantFirst)
			if !finished {
				secondErr = <-second
			}
			wantResult(t, "second", secondErr, tc.wantSecond)
			tc.check(t, c, f)
		})
	}
}

// exemptFromParkedPairs lists callers of beginModify that have no row, with the reason. A caller
// is added here only when no other call can touch what it reads and writes.
var exemptFromParkedPairs = map[string]string{}

// beginModifyCallers returns the names of the methods and functions of the package that call
// beginModify, found by reading the sources (the tests are skipped).
func beginModifyCallers(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("cannot list the package sources: %v", err)
	}
	var callers []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && callsBeginModify(fn.Body) {
				callers = append(callers, fn.Name.Name)
			}
		}
	}
	sort.Strings(callers)
	return callers
}

func callsBeginModify(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "beginModify" {
				found = true
			}
		}
		return !found
	})
	return found
}

// A method that reads a setting and writes it back under c.modify needs a test that holds it
// between the two and runs a partner: a new one without a row of parkedPairs fails here, by name.
func TestEveryReadModifyWriteMethodHasAParkedPairTest(t *testing.T) {
	callers := beginModifyCallers(t)
	if len(callers) < 6 {
		t.Fatalf("the scan found only %v: it no longer sees the callers of beginModify", callers)
	}
	rows := map[string]bool{}
	for _, row := range parkedPairs { // subtests: the loop builds the set of rows the callers are compared with
		rows[row.first.String()] = true
		if row.second == "" || row.callFirst == nil || row.callSecond == nil || row.check == nil {
			t.Errorf("row %q is incomplete", row.name)
		}
	}
	known := map[string]bool{}
	for _, name := range callers {
		known[name] = true
		t.Run("caller "+name, func(t *testing.T) {
			_, exempt := exemptFromParkedPairs[name]
			switch {
			case !rows[name] && !exempt:
				t.Errorf("%s reads and writes under c.modify (it calls beginModify) and has no row in parkedPairs: add the partner that touches the same request or state, "+
					"or name it in exemptFromParkedPairs with the reason", name)
			case rows[name] && exempt:
				t.Errorf("%s has a row and is also exempt: remove one", name)
			}
		})
	}
	t.Run("no row without a caller", func(t *testing.T) {
		for name := range rows { // subtests: one check over the whole table, named in its message
			if !known[name] {
				t.Errorf("parkedPairs has a row for %s, which does not call beginModify (any more): remove the row", name)
			}
		}
	})
	t.Run("every exemption is current and has a reason", func(t *testing.T) {
		for name, reason := range exemptFromParkedPairs { // subtests: one check over the whole list, named in its message
			if !known[name] || reason == "" {
				t.Errorf("exemptFromParkedPairs lists %q, which is not a caller of beginModify or has no reason", name)
			}
		}
	})
}
