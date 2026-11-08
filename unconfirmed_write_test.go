package escp

import (
	"context"
	"errors"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/model"
)

// A write the switch never answers may have been applied, and how often it was sent is part of
// what the caller is told: Retries+1 sends per run (dialFast: 2), twice that when the run is
// repeated after a new session key, which only a write that sets an absolute value is.
func TestAWriteTheSwitchNeverAnswersIsUnconfirmedAndSentAsOftenAsItsImpactAllows(t *testing.T) {
	const sendsPerRun = 2 // dialFast: one retry
	tests := []struct {
		name   string
		method model.Method
		write  func(*Client) error
		sends  int
	}{
		{"cosmetic: repeated after the new session key", model.MethodSetLED, func(c *Client) error { return c.SetLED(context.Background(), false) }, 2 * sendsPerRun},
		{"traffic: repeated after the new session key", model.MethodSetQoSMode, func(c *Client) error { return c.SetQoSMode(context.Background(), model.QoSPortBased) }, 2 * sendsPerRun},
		{"management: not repeated", model.MethodSetLoopPrevention, func(c *Client) error { return c.SetLoopPrevention(context.Background(), true) }, sendsPerRun},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := escptest.Start(t)
			c := dialFast(t, f, "secret")
			if _, err := c.LED(context.Background()); err != nil { // the session exists before the switch goes quiet
				t.Fatal(err)
			}
			f.Configure(func(f *escptest.Faults) { f.DropSets = true })
			err := tt.write(c)
			var unconfirmed *UnconfirmedWriteError
			if !errors.As(err, &unconfirmed) || unconfirmed.Method != tt.method {
				t.Fatalf("want an *UnconfirmedWriteError for %s, got %v", tt.method, err)
			}
			if !errors.Is(err, ErrWriteUnconfirmed) || !errors.Is(err, ErrNoReply) {
				t.Fatalf("the error must match ErrWriteUnconfirmed and ErrNoReply: %v", err)
			}
			if got := f.DroppedSets(); got != tt.sends {
				t.Fatalf("the switch received the write %d times, want %d", got, tt.sends)
			}
		})
	}
}

// A management write that was not repeated drops the session, so the next call keys again and
// works: the client does not stay stuck with a key the switch no longer knows.
func TestAfterAnUnconfirmedManagementWriteTheNextCallKeysAgain(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	if err := c.SetLoopPrevention(context.Background(), true); !errors.Is(err, ErrWriteUnconfirmed) {
		t.Fatalf("want an unconfirmed write, got %v", err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = false })
	if err := c.SetLoopPrevention(context.Background(), true); err != nil {
		t.Fatalf("the next write must key again and succeed: %v", err)
	}
}

// A write that never left the client is not unconfirmed: a switch that does not answer the key
// exchange gives a plain ErrNoReply, and the write is not sent.
func TestAWriteWhoseSessionKeyIsNotAnsweredIsNotUnconfirmed(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	f.Mute()
	err := c.SetLED(context.Background(), false)
	if !errors.Is(err, ErrNoReply) || errors.Is(err, ErrWriteUnconfirmed) {
		t.Fatalf("want a plain ErrNoReply, got %v", err)
	}
}

func TestRepeatableFailsClosedForAMethodOutsideTheTable(t *testing.T) {
	if repeatable(model.Method("SetNothing")) {
		t.Fatal("a method without a class must not be repeated")
	}
}
