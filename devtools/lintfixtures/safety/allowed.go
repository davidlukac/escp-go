// Package safety stands for the safety package of the library in the lint fixtures.
package safety

import "github.com/davidlukac/escp-go/model"

// Name is what the other fixtures read.
const Name = "safety"

// Describe imports what the safety layer may: the model. No finding.
func Describe() string { return model.Describe() }
