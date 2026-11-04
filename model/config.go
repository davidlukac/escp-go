package model

import (
	"context"
)

// Reader is the part of a Client that a WriteGate may use: the reads of the settings, so that a gate
// can tell what a write would disturb (which ports have a link, which belong to a group, which firmware
// runs). It only reads. A *Client is a Reader; a test double implements the few methods its gate calls
// and embeds the interface for the rest.
type Reader interface {
	// SwitchMAC is the MAC address of the switch the reader talks to; zero while it is not known
	// yet. It never waits for a call in flight.
	SwitchMAC() MAC
	Info(ctx context.Context) (Info, error)
	System(ctx context.Context) (System, error)
	LED(ctx context.Context) (bool, error)
	LoopPrevention(ctx context.Context) (bool, error)
	Ports(ctx context.Context) ([]Port, error)
	LAGs(ctx context.Context) ([]LAG, error)
	PortMirror(ctx context.Context) (Mirror, error)
	Bandwidth(ctx context.Context) ([]PortBandwidth, error)
	StormControl(ctx context.Context) ([]PortStorm, error)
	QoS(ctx context.Context) (QoS, error)
	IGMP(ctx context.Context) (IGMP, error)
	VLANMode(ctx context.Context) (VLANModeSetting, error)
	MTUVLAN(ctx context.Context) (MTUVLAN, error)
	PortBasedVLAN(ctx context.Context) (PortBasedVLAN, error)
	Dot1Q(ctx context.Context) (Dot1Q, error)
}

// WriteGate decides whether the client may send a write to the switch. Every
// method that changes the switch (each Set* and Delete*) calls it first, with
// the client itself and a description of the write (its method, the ports it
// names and its Impact), and sends nothing when it returns an error: the
// method returns that error unchanged. A gate may read from the switch through
// sw but must never write, and must be safe for concurrent use. Reads are
// never gated.
//
// A method that sends several writes consults the gate before each of them
// with the same operation, so a gate that is expensive should remember its
// verdict.
type WriteGate func(ctx context.Context, sw Reader, op Operation) error
