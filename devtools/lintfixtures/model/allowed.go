// Package model stands for the model package of the library in the lint fixtures.
package model

import (
	"fmt"

	"github.com/davidlukac/escp-go/internal/invalid"
	"github.com/davidlukac/escp-go/internal/wire"
)

// Describe imports what the model may: the standard library and the two internal packages. No finding.
func Describe() string { return fmt.Sprint(wire.Name, invalid.Name) }
