package safety

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/davidlukac/escp-go/model"
)

// A DryRun is safe for concurrent use, as its documentation says: gates run on several goroutines while
// another reads the record (run with -race), and every operation is recorded once.
//
// guarantee: DryRun
func TestADryRunIsSafeForConcurrentUse(t *testing.T) {
	const goroutines, writes = 8, 25
	d := NewDryRun()
	gate := d.Gate()
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range writes {
				if err := gate(context.Background(), nil, model.Operation{Name: model.MethodSetLED}); !errors.Is(err, ErrDryRun) {
					t.Errorf("gate = %v, want ErrDryRun", err)
				}
				_ = d.Operations()
			}
		})
	}
	wg.Wait()
	if got := len(d.Operations()); got != goroutines*writes {
		t.Errorf("%d operations recorded, want %d", got, goroutines*writes)
	}
}
