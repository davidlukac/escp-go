// Command demo stands for an example program: the forbidigo rules leave it out, and it may use the fake
// switch. No finding.
package main

import (
	"fmt"

	"github.com/davidlukac/escp-go/escptest"
)

func main() {
	fmt.Println(escptest.Name)
}
