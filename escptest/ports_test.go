package escptest

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
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
	// The port handed out first is refused, so a second try was needed; more than two is fine, because
	// the kernel may draw a port that another test of the package was given.
	if got == first || calls < 2 {
		t.Errorf("got port %d after %d tries, want a port other than %d after at least two tries", got, calls, first)
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
	// a port that is free for UDP, and the same TCP number held by a listener that stands in for
	// another process's marker (the system may forbid that TCP number: the test cannot run then)
	probe, err := listenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	ap, err := udp.AddrPort(probe.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	held := int(ap.Port())
	_ = probe.Close() // best effort: only the number is kept
	other, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(held)))
	if markerForbidden(err) {
		t.Skipf("the system forbids the TCP port %d that this test needs to hold: %v", held, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close() }() // best effort at the end of the test
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
	// the marker fails the way it does when another process holds the TCP number; a real held
	// listener would depend on the ports the machine lets the test hold (a Windows runner forbids some)
	markerWith(t, func(port int) (net.Listener, error) {
		return nil, &net.OpError{Op: "listen", Net: "tcp4", Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}, Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}
	})
	_, _, err := listenUnreserved(listenLoopback)
	if err == nil || !strings.Contains(err.Error(), "listen tcp4 127.0.0.1:") || !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("got %v, want the failed TCP listen of the marker in the message and in the chain", err)
	}
}

// forbidden is the error a Windows socket gives for a port it will not bind (WSAEACCES, 10013,
// written out here so that a wrong number in the code is caught).
func forbidden() error {
	return &net.OpError{Op: "listen", Net: "tcp4", Err: os.NewSyscallError("bind", syscall.Errno(10013))}
}

// markerWith makes reserve use listen for the test and puts the real one back.
func markerWith(t *testing.T, listen func(port int) (net.Listener, error)) {
	t.Helper()
	saved := listenMarker
	listenMarker = listen
	t.Cleanup(func() { listenMarker = saved })
}

// Where the system forbids the TCP number the port is handed out without a marker, and this
// process still never hands it out twice.
func TestAPortThatCannotBeMarkedIsHandedOutOnceHere(t *testing.T) {
	markerWith(t, func(int) (net.Listener, error) { return nil, forbidden() })
	c, first, err := listenUnreserved(listenLoopback)
	if err != nil {
		t.Fatalf("a port the system forbids to mark was not handed out: %v", err)
	}
	_ = c.Close() // best effort at the end of the test
	calls := 0
	listen := func() (*net.UDPConn, error) {
		calls++
		if calls == 1 {
			return listenAt(t, first)() // the kernel offers the port just handed out first
		}
		return listenLoopback()
	}
	c, got, err := listenUnreserved(listen)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }() // best effort at the end of the test
	// The port handed out first is refused, so a second try was needed; more than two is fine, because
	// the kernel may draw a port that another test of the package was given.
	if got == first || calls < 2 {
		t.Errorf("got port %d after %d tries, want a port other than %d after at least two tries", got, calls, first)
	}
}

// Any other failure of the marker is a port that somebody holds: it is skipped, and the search
// gives up with the reason when every try fails.
func TestAPortWhoseMarkerFailsForAnotherReasonIsSkipped(t *testing.T) {
	markerWith(t, func(int) (net.Listener, error) { return nil, syscall.EADDRINUSE })
	_, _, err := listenUnreserved(listenLoopback)
	if !errors.Is(err, syscall.EADDRINUSE) || !strings.Contains(err.Error(), "no unreserved UDP port after 100 tries") {
		t.Fatalf("got %v, want the search to give up with the reason", err)
	}
}

func TestOnlyTheForbiddenErrorOfTheSystemIsTakenForAMarkerThatCannotBePlaced(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"the Windows socket error, wrapped as the net package does": {forbidden(), true},
		"address in use":     {&net.OpError{Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}, false},
		"permission on Unix": {&net.OpError{Err: os.NewSyscallError("bind", syscall.EACCES)}, false},
		"some other error":   {errors.New("boom"), false},
		"no error":           {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := markerForbidden(tc.err); got != tc.want {
				t.Errorf("%s: markerForbidden = %v, want %v", name, got, tc.want)
			}
		})
	}
}

// The ports come from a range the kernel never draws from for a socket that binds port 0, so no
// socket of the process can be given a port that was just handed out.
func TestPortsAreHandedOutFromBelowTheKernelsOwnRange(t *testing.T) {
	for range 200 {
		port := FreeUDPPort(t)
		if port < firstTestPort || port >= endTestPort {
			t.Fatalf("port %d is outside %d-%d", port, firstTestPort, endTestPort-1)
		}
		if port == 29808 || port == 29809 {
			t.Fatalf("port %d is one of the protocol's own", port)
		}
	}
}

func TestTheSearchForALoopbackPortGivesUpAndSaysSo(t *testing.T) {
	cause := errors.New("address already in use")
	tries := 0
	old := listenUDP
	listenUDP = func(string, *net.UDPAddr) (*net.UDPConn, error) { tries++; return nil, cause }
	t.Cleanup(func() { listenUDP = old })
	_, err := listenLoopback()
	if !errors.Is(err, cause) || tries != maxLoopbackTries {
		t.Fatalf("got %v after %d tries, want %q after %d", err, tries, cause, maxLoopbackTries)
	}
}
