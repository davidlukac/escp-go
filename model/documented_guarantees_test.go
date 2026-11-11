package model

import "testing"

// The tests below pin sentences of doc comments that no other test observed from outside (defect
// class DC-C01). Each carries a `// guarantee: <Name>` marker that `make guarantee` looks for.

// guarantee: Firmware.UnmarshalText
//
// UnmarshalText reads a version string like ParseFirmware and never fails, whatever the text is: a text
// that is no vendor version is a firmware without a build.
func TestFirmwareUnmarshalTextNeverFailsAndReadsLikeParseFirmware(t *testing.T) {
	tests := []struct {
		name string
		text []byte
	}{
		{"a vendor version", []byte("1.0.0 Build 20230218 Rel.50633")},
		{"a text that is no version", []byte("something else")},
		{"an empty text", []byte{}},
		{"no text", nil},
		{"bytes that are no text", []byte{0xff, 0xfe, 0x00}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got Firmware
			if err := got.UnmarshalText(tc.text); err != nil {
				t.Fatalf("UnmarshalText failed: %v", err)
			}
			if want := ParseFirmware(string(tc.text)); got != want {
				t.Errorf("UnmarshalText = %+v, ParseFirmware = %+v", got, want)
			}
		})
	}
}

// guarantee: StormConfig.Validate
//
// Validate accepts a traffic type with a rate from one step up to MaxBandwidth and refuses what is
// above it, and refuses a rate that is no multiple of the step; with no traffic type the rate must be 0.
func TestStormConfigValidateBoundsTheRate(t *testing.T) {
	runValidate(t, []validateCase{
		{"the highest rate", StormConfig{Port: 1, Multicast: true, Rate: MaxBandwidth}, false},
		{"one step above the highest", StormConfig{Port: 1, Multicast: true, Rate: MaxBandwidth + RateStep}, true},
		{"a rate just below a step", StormConfig{Port: 1, Multicast: true, Rate: RateStep - 1}, true},
		{"a negative rate", StormConfig{Port: 1, Multicast: true, Rate: -RateStep}, true},
		{"a negative rate with no type", StormConfig{Port: 1, Rate: -RateStep}, true},
		{"the highest port", StormConfig{Port: MaxPort, Broadcast: true, Rate: RateStep}, false},
		{"a port above the highest", StormConfig{Port: MaxPort + 1, Broadcast: true, Rate: RateStep}, true},
	})
}
