package invalid

import (
	"errors"
	"testing"
)

func TestErrorfMatchesTheClassAndReadsAsTheMessage(t *testing.T) {
	err := Errorf("port %d is not valid", 9)
	if !errors.Is(err, Err) || err.Error() != "port 9 is not valid" {
		t.Fatalf("got %v", err)
	}
	if errors.Is(errors.New("other"), Err) {
		t.Fatal("another error is no invalid argument")
	}
}
