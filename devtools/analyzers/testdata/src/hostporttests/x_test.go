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
