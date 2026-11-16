package escp

import "time"

// WaitWithAfter uses time.After, which keeps its timer until it fires.
func WaitWithAfter() {
	<-time.After(time.Second)
}
