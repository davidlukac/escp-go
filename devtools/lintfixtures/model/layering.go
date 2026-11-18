package model

import escp "github.com/davidlukac/escp-go"

// ReachUp imports the root package from the model, against the arrows of the layering: the model-layer
// rule refuses it.
func ReachUp() string { return escp.Name }
