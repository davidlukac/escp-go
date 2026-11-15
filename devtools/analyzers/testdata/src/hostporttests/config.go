// Package hostporttests holds the configuration type the hostport check is pointed at in the fixtures.
package hostporttests

// Config is the configuration of a client: SwitchPort is where the switch (or the fake) listens, HostPort
// the local port the replies come to.
type Config struct {
	Host       string
	SwitchPort int
	HostPort   int
}

// Client is made from a Config.
type Client struct{ cfg Config }

// Other has the same fields under another name: the check looks at the type it is told to.
type Other struct {
	SwitchPort int
	HostPort   int
}

// freePort stands in for the test allocator.
func freePort() int { return 40000 }

// Dial takes the configuration as it is: it leaves the reply port as the caller set it.
func Dial(cfg Config) *Client { return &Client{cfg: cfg} }

// withPort gives the client a port of its own when the caller named none, like a test helper of the
// library does: a function that assigns the HostPort of its parameter owns the port.
func withPort(cfg Config) *Client {
	if cfg.HostPort == 0 {
		cfg.HostPort = freePort()
	}
	return Dial(cfg)
}

// withPortPointer is the same for a pointer parameter.
func withPortPointer(cfg *Config) *Client {
	cfg.HostPort = freePort()
	return Dial(*cfg)
}

// With is a method that owns the port: it assigns the HostPort of its parameter.
func (c *Client) With(cfg Config) *Client {
	cfg.HostPort = freePort()
	c.cfg = cfg
	return c
}

// withHost assigns another field of its parameter: it does not give the client a port.
func withHost(cfg Config) *Client {
	cfg.Host = "127.0.0.1"
	return Dial(cfg)
}

// pair returns two values, for an assignment with one call on its right.
func pair() (Config, error) { return Config{}, nil }
