// Command coverpolicy stands for a program of the library: the forbidigo rules leave it out, so it may
// print and use time.After. No finding.
package main

import (
	"fmt"
	"log"
	"time"
)

func main() {
	<-time.After(time.Nanosecond)
	fmt.Println("report")
	log.Print("report")
}
