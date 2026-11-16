package hostporttests

import "testing"

// The shape of escp-go#237: a literal aimed at the fake, with no reply port of its own.
func TestAimedAtTheFakeWithoutAPort(t *testing.T) {
	_ = Dial(Config{Host: "127.0.0.1", SwitchPort: 29808})               // want `binds the real reply port 29809`
	_ = &Config{Host: "127.0.0.1", SwitchPort: 29808}                    // want `binds the real reply port 29809`
	cfg := Config{Host: "127.0.0.1", SwitchPort: 29808}                  // want `binds the real reply port 29809`
	_ = []Config{{Host: "a", SwitchPort: 1}, {Host: "b", SwitchPort: 2}} // want `binds the real reply port` `binds the real reply port`
	_ = cfg
	// A literal inside a closure is looked at as well.
	func() {
		_ = Config{SwitchPort: 29808} // want `binds the real reply port 29809`
	}()
}

// A literal that sets both ports, or aims at nothing (no SwitchPort), is fine.
func TestWithAPortOfItsOwn(t *testing.T) {
	_ = Dial(Config{Host: "127.0.0.1", SwitchPort: 29808, HostPort: freePort()})
	_ = Dial(Config{Host: "127.0.0.1", HostPort: 5})
	_ = Dial(Config{Host: "192.0.2.1"})
	_ = Dial(Config{})
}

// The port is assigned to the variable the literal initializes, in the same function.
func TestThePortIsAssignedAfterwards(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", SwitchPort: 29808}
	cfg.HostPort = freePort()
	_ = Dial(cfg)

	pointer := &Config{Host: "127.0.0.1", SwitchPort: 29808}
	pointer.HostPort = freePort()
	_ = Dial(*pointer)
}

// An assignment to another variable's port does not excuse this one.
func TestThePortOfAnotherVariable(t *testing.T) {
	cfg := Config{SwitchPort: 29808} // want `binds the real reply port 29809`
	other := Config{}
	other.HostPort = freePort()
	_, _ = cfg, other
}

// A function of the package that assigns the port of its parameter gives the client its port: a literal
// handed to it, directly or through a variable, is fine; one handed to a function that does not is not.
func TestHandedToAFunctionThatOwnsThePort(t *testing.T) {
	_ = withPort(Config{Host: "127.0.0.1", SwitchPort: 29808})
	_ = withPortPointer(&Config{Host: "127.0.0.1", SwitchPort: 29808})
	cfg := Config{Host: "127.0.0.1", SwitchPort: 29808}
	_ = withPort(cfg)
	_ = Dial(Config{SwitchPort: 29808}) // want `binds the real reply port 29809`
	kept := Config{SwitchPort: 29808}   // want `binds the real reply port 29809`
	_ = Dial(kept)
}

// Another type with the same fields is not the configuration.
func TestAnotherType(t *testing.T) {
	_ = Other{SwitchPort: 29808}
}

// A literal that needs the real port says why, on its line or the line above.
func TestAnnotated(t *testing.T) {
	_ = Dial(Config{SwitchPort: 29808}) // hostport: the default port is the thing under test
	// hostport: the test binds the real port to prove the sharing
	_ = Dial(Config{SwitchPort: 29808})
}

// An annotation without a reason is reported once, and still excuses the literal.
func TestAnnotationWithoutAReason(t *testing.T) {
	/* hostport: */ // want `a hostport annotation needs a reason`
	_ = Dial(Config{SwitchPort: 29808})
}

// hostport: no case dials, the table only validates values.
func TestAnnotatedByTheDocComment(t *testing.T) {
	_ = Config{SwitchPort: 1}
	_ = Config{SwitchPort: 2}
}

var tableOfConfigs = []Config{
	{Host: "a", SwitchPort: 1}, // want `binds the real reply port 29809`
}

// A method that owns the port is found at its call, and so is the assignment of another field not taken for
// the port's.
func TestMethodsAndOtherFields(t *testing.T) {
	c := &Client{}
	_ = c.With(Config{SwitchPort: 29808})
	_ = withHost(Config{SwitchPort: 29808}) // want `binds the real reply port 29809`
	named := Config{SwitchPort: 29808}      // want `binds the real reply port 29809`
	named.Host = "127.0.0.1"
	_ = Dial(named)
	got, err := pair() // an assignment of two values from one call
	_, _ = got, err
}

// A function that assigns a constant to the HostPort of its parameter does not give the client a port.
func TestConstantsAreNotAPort(t *testing.T) {
	_ = withConstantPort(Config{SwitchPort: 29808}) // want `binds the real reply port 29809`
	_ = withZeroedPort(Config{SwitchPort: 29808})   // want `binds the real reply port 29809`
	cfg := Config{SwitchPort: 29808}                // want `binds the real reply port 29809`
	cfg.HostPort = 29809
	_ = Dial(cfg)
	other := Config{SwitchPort: 29808} // want `binds the real reply port 29809`
	other.HostPort = 0
	_ = Dial(other)
}

// A configuration built by assigning its fields is judged the same way: the port must be there before the
// first use.
func TestBuiltByAssignment(t *testing.T) {
	var bare Config
	bare.Host = "127.0.0.1"
	bare.SwitchPort = 29808 // want `binds the real reply port 29809`
	_ = Dial(bare)

	var good Config
	good.Host = "127.0.0.1"
	good.SwitchPort = 29808
	good.HostPort = freePort()
	_ = Dial(good)

	var pointer = &Config{}
	pointer.SwitchPort = 29808 // want `binds the real reply port 29809`
	_ = Dial(*pointer)

	withPortFirst := &Config{}
	withPortFirst.HostPort = freePort()
	withPortFirst.SwitchPort = 29808
	_ = Dial(*withPortFirst)

	var handed Config
	handed.SwitchPort = 29808
	_ = withPort(handed) // the function gives the port

	var noSwitch Config // not aimed at the fake: no SwitchPort
	noSwitch.Host = "192.0.2.1"
	_ = Dial(noSwitch)

	var zero Config
	zero.SwitchPort = 29808 // want `binds the real reply port 29809`
	zero.HostPort = 29809
	_ = Dial(zero)
}

// The order matters: a port assigned after the configuration was used is too late.
func TestThePortComesAfterTheFirstUse(t *testing.T) {
	cfg := Config{SwitchPort: 29808} // want `binds the real reply port 29809`
	_ = Dial(cfg)
	cfg.HostPort = freePort()

	var late Config
	late.SwitchPort = 29808 // want `binds the real reply port 29809`
	_ = []Config{late}
	late.HostPort = freePort()

	var returned Config
	returned.SwitchPort = 29808 // want `binds the real reply port 29809`
	func() Config { return returned }()
	returned.HostPort = freePort()

	var copied Config
	copied.SwitchPort = 29808 // want `binds the real reply port 29809`
	again := copied
	copied.HostPort = freePort()
	_ = again

	var asPointer Config
	asPointer.SwitchPort = 29808 // want `binds the real reply port 29809`
	_ = &asPointer
	asPointer.HostPort = freePort()

	// Reading or writing a field is not a use, and the port comes before the call.
	var early Config
	early.SwitchPort = 29808
	_ = early.Host
	early.Host = "127.0.0.1"
	early.HostPort = freePort()
	_ = Dial(early)

	// A literal that sets its port is ported from its start.
	ported := Config{SwitchPort: 29808, HostPort: freePort()}
	_ = Dial(ported)
}

// An assignment of SwitchPort carries the annotation on its line or the line above it.
func TestAnnotatedAssignment(t *testing.T) {
	var cfg Config
	cfg.SwitchPort = 29808 // hostport: this test binds the default on purpose
	_ = Dial(cfg)

	var above Config
	// hostport: this one too
	above.SwitchPort = 29808
	_ = Dial(above)
}

// A configuration that only a variable of another function was aimed at is not this one's business.
func TestNotAimed(t *testing.T) {
	cfg := make2()
	_ = Dial(cfg)
	cfg2 := Config{Host: "x"}
	_ = Dial(cfg2)
}

// The right side of a port assignment is evaluated before the port is there: a use in it is a use.
func TestTheUseIsOnTheRightOfTheAssignment(t *testing.T) {
	var cfg Config
	cfg.SwitchPort = 29808 // want `binds the real reply port 29809`
	cfg.HostPort = portFor(cfg)

	var viaOwner Config // the function that owns the port is the first use, and gives it
	viaOwner.SwitchPort = 29808
	viaOwner.HostPort = withPort(viaOwner).cfg.HostPort

	var tuple Config // an assignment of two values from one call
	tuple.SwitchPort = 29808
	var err error
	tuple.HostPort, err = twoInts()
	_ = err
	_ = Dial(tuple)
}

// A configuration that is aimed, given a port and never handed on is fine; the first SwitchPort assignment
// is where a report goes.
func TestNeverUsedAndAssignedTwice(t *testing.T) {
	var kept Config
	kept.SwitchPort = 29808
	kept.HostPort = freePort()
	_ = kept.Host

	var twice Config
	twice.SwitchPort = 29808 // want `binds the real reply port 29809`
	twice.SwitchPort = 29809
	_ = Dial(twice)
}

// The target of an assignment is not a use, and a second literal does not replace the first.
func TestReassigned(t *testing.T) {
	var cfg Config
	cfg = Config{SwitchPort: 29808, HostPort: freePort()}
	_ = Dial(cfg)

	first := Config{SwitchPort: 29808} // want `binds the real reply port 29809`
	_ = Dial(first)
	first = Config{HostPort: freePort()}
	_ = Dial(first)
}

// Another type with the fields is not the configuration, as a variable either, and a function that assigns
// the port of a parameter that is not one does not own the port of the configuration it is given.
func TestOtherTypesAndParameters(t *testing.T) {
	var o Other
	o.SwitchPort = 29808
	_ = o
	_ = shifted(Other{}, Config{SwitchPort: 29808}) // want `binds the real reply port 29809`
}
