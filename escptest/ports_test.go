package escptest

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/internal/udp"
)

// listenAt binds the loopback port the test names, standing in for a kernel that hands a released
// port out again.
func listenAt(t *testing.T, port int) func() (*net.UDPConn, error) {
	t.Helper()
	return func() (*net.UDPConn, error) {
		return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	}
}

func TestAPortHandedOutIsNotGivenAgain(t *testing.T) {
	first := FreeUDPPort(t) // released, but reserved
	calls := 0
	// the kernel offers the port just released first, then a fresh one
	listen := func() (*net.UDPConn, error) {
		calls++
		if calls == 1 {
			return listenAt(t, first)()
		}
		return listenLoopback()
	}
	c, got, err := listenUnreserved(listen)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }() // best effort at the end of the test
	if got == first || calls != 2 {
		t.Errorf("got port %d after %d tries, want a port other than %d on the second try", got, calls, first)
	}
	if bound, err := udp.AddrPort(c.LocalAddr()); err != nil || int(bound.Port()) != got {
		t.Errorf("the reported port is %d, the socket is bound to %v (%v)", got, bound, err)
	}
}

func TestStartTakesAPortNobodyWasGiven(t *testing.T) {
	given := FreeUDPPort(t)
	sw := Start(t)
	if sw.UDPPort() == given {
		t.Errorf("the fake switch took port %d, which FreeUDPPort had handed out", given)
	}
}

func TestSearchForAPortGivesUpAndSaysSo(t *testing.T) {
	held := FreeUDPPort(t)
	_, _, err := listenUnreserved(listenAt(t, held))
	if err == nil {
		t.Fatal("expected an error when every offered port was handed out before")
	}
}

func TestListenErrorIsReturned(t *testing.T) {
	blocker, err := listenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Close() }() // best effort at the end of the test
	blockerAddr, err := udp.AddrPort(blocker.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	port := int(blockerAddr.Port())
	// binding a port that is in use fails; the error comes back and nothing is reserved for it
	if _, _, err := listenUnreserved(listenAt(t, port)); err == nil {
		t.Fatal("expected the bind error")
	}
}

// The processes of one `go test ./...` run in parallel on one machine, and each asks for ports: a
// port released by one must not be handed to another while the first is still going to use it.
// Four child processes (this test binary, run as helpers) reserve many ports each and stay alive;
// no port may appear twice.
func TestPortsAreReservedAcrossProcesses(t *testing.T) {
	const children, portsEach = 4, 200
	type child struct {
		cmd   *exec.Cmd
		stdin io.WriteCloser
		ports []int
	}
	var started []*child
	for range children {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperReservingPorts$", "-test.count=1")
		cmd.Env = append(os.Environ(), helperPortsEnv+"="+strconv.Itoa(portsEach))
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		c := &child{cmd: cmd, stdin: stdin}
		started = append(started, c)
		t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Wait() }) // best effort: the children end when their stdin closes
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := sc.Text()
			if line == "ready" {
				break
			}
			if n, err := strconv.Atoi(line); err == nil {
				c.ports = append(c.ports, n)
			}
		}
		if len(c.ports) != portsEach {
			t.Fatalf("a child reserved %d ports, want %d (%v)", len(c.ports), portsEach, sc.Err())
		}
	}
	// Every child is alive and holds its ports now.
	owner := map[int]int{}
	for i, c := range started {
		for _, p := range c.ports {
			if j, taken := owner[p]; taken {
				t.Fatalf("port %d was handed to process %d and to process %d, both alive", p, j, i)
			}
			owner[p] = i
		}
	}
}

// helperPortsEnv makes TestHelperReservingPorts act as a child of TestPortsAreReservedAcrossProcesses:
// its value is the number of ports to reserve.
const helperPortsEnv = "ESCPTEST_HELPER_PORTS"

// TestHelperReservingPorts is not a test: it is the child process of
// TestPortsAreReservedAcrossProcesses. It prints the ports it reserved, then "ready", and exits when
// its standard input closes.
func TestHelperReservingPorts(t *testing.T) {
	n, err := strconv.Atoi(os.Getenv(helperPortsEnv))
	if err != nil {
		t.Skip("only runs as a child of TestPortsAreReservedAcrossProcesses")
	}
	for range n {
		fmt.Println(FreeUDPPort(t))
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin) // returns when the parent closes the pipe
}

// A port whose TCP number somebody else listens on is another process's reservation (or something
// unrelated); the search moves on to a port that is free in both.
func TestAPortThatAnotherProcessHoldsIsSkipped(t *testing.T) {
	other, err := net.Listen("tcp4", "127.0.0.1:0") // stands in for another process's marker
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }()     // best effort at the end of the test
	held := other.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // a TCP listener has a TCP address
	calls := 0
	listen := func() (*net.UDPConn, error) {
		calls++
		if calls == 1 {
			return listenAt(t, held)() // the kernel offers the held port first
		}
		return listenLoopback()
	}
	c, got, err := listenUnreserved(listen)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }() // best effort at the end of the test
	if got == held || calls != 2 {
		t.Errorf("got port %d after %d tries, want a port other than %d on the second try", got, calls, held)
	}
}

// When nothing can be reserved, the error says why.
func TestTheReasonForNotFindingAPortIsKept(t *testing.T) {
	held := FreeUDPPort(t)
	_, _, err := listenUnreserved(listenAt(t, held))
	if err == nil || !strings.Contains(err.Error(), "listen tcp4 127.0.0.1:") {
		t.Fatalf("got %v, want the failed TCP listen of the marker in the message", err)
	}
}
