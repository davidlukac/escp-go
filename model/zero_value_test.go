package model

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Defect class DC-L05 (a decoder and the writer's rule must agree): the zero value of a record is a
// reading ("nothing set, nothing read"), so it must read back from its own encoding. Speed and
// VLANMode refused the empty text, so the zero Port, PortConfig, PortStats and VLANModeSetting did not.

// zeroRecords holds the zero value of every exported struct of the package that round trips through
// JSON. The scan of the source below fails when a struct is missing here and in zeroRoundTripExceptions,
// so a new record has to be added to one of them.
var zeroRecords = map[string]any{
	"Dot1Q":           Dot1Q{},
	"Dot1QVLAN":       Dot1QVLAN{},
	"Firmware":        Firmware{},
	"Group":           Group{},
	"IGMP":            IGMP{},
	"IGMPConfig":      IGMPConfig{},
	"IGMPGroup":       IGMPGroup{},
	"IPConfig":        IPConfig{},
	"Info":            Info{},
	"LAG":             LAG{},
	"MTUVLAN":         MTUVLAN{},
	"Mirror":          Mirror{},
	"Port":            Port{},
	"PortBandwidth":   PortBandwidth{},
	"PortBasedVLAN":   PortBasedVLAN{},
	"PortConfig":      PortConfig{},
	"PortPVID":        PortPVID{},
	"PortPriority":    PortPriority{},
	"PortStats":       PortStats{},
	"PortStorm":       PortStorm{},
	"PortVLAN":        PortVLAN{},
	"QoS":             QoS{},
	"Setting":         Setting{},
	"StormConfig":     StormConfig{},
	"System":          System{},
	"VLANModeSetting": VLANModeSetting{},
}

// zeroRoundTripExceptions are the structs whose zero value does not round trip, each with the reason.
// An entry is checked to still fail (TestTheZeroRoundTripExceptionsStillFail), so it goes stale loudly.
var zeroRoundTripExceptions = map[string]struct {
	value  any
	reason string
}{
	"Operation": {Operation{}, "the empty Method has no name: Method.MarshalText refuses it by design, so that no operation without a method is written down"},
	"WriteMethod": {WriteMethod{}, "the empty Method has no name: Method.MarshalText refuses it by design (the settings table lists " +
		"methods that exist; it is documentation, not a reading)"},
}

// exportedStructs lists the exported struct types the package declares outside its tests, read from
// the source so that a type added there is checked without being added here.
func exportedStructs(t *testing.T) []string {
	t.Helper()
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			if spec, ok := n.(*ast.TypeSpec); ok && spec.Name.IsExported() {
				if _, isStruct := spec.Type.(*ast.StructType); isStruct {
					out = append(out, spec.Name.Name)
				}
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

// Every exported struct is in exactly one of the two tables, and no table names a struct that is gone.
func TestEveryExportedRecordIsCheckedForItsZeroValueRoundTrip(t *testing.T) {
	structs := exportedStructs(t)
	if len(structs) < 25 {
		t.Fatalf("found only %d exported structs; the scan is broken", len(structs))
	}
	declared := map[string]bool{}
	for _, name := range structs {
		declared[name] = true
		t.Run(name, func(t *testing.T) {
			_, inRecords := zeroRecords[name]
			_, inExceptions := zeroRoundTripExceptions[name]
			switch {
			case inRecords && inExceptions:
				t.Errorf("%s is in both tables", name)
			case !inRecords && !inExceptions:
				t.Errorf("%s has no row in zeroRecords or zeroRoundTripExceptions: add its zero value and see whether it reads back", name)
			}
		})
	}
	var listed []string
	for name := range zeroRecords {
		listed = append(listed, name)
	}
	for name := range zeroRoundTripExceptions {
		listed = append(listed, name)
	}
	for _, name := range listed {
		t.Run("listed "+name, func(t *testing.T) {
			if !declared[name] {
				t.Errorf("the tables list %s, which the package does not declare as an exported struct", name)
			}
		})
	}
}

// guarantee: Speed.UnmarshalText
// guarantee: VLANMode.UnmarshalText
//
// The zero value of every record reads back from its own JSON: the same mechanism as
// TestReadingsSurviveJSON, over every exported struct instead of a hand-picked list.
func TestTheZeroValueOfEveryRecordSurvivesJSON(t *testing.T) {
	for name, zero := range zeroRecords {
		t.Run(name, func(t *testing.T) {
			text, err := json.Marshal(zero)
			if err != nil {
				t.Fatal(err)
			}
			into := reflect.New(reflect.TypeOf(zero))
			if err := json.Unmarshal(text, into.Interface()); err != nil {
				t.Fatalf("decoding %s: %v", text, err)
			}
			if got := into.Elem().Interface(); !reflect.DeepEqual(got, zero) {
				t.Errorf("after %s\ngot  %+v\nwant %+v", text, got, zero)
			}
		})
	}
}

// An exception is only an exception while it fails to round trip: when the cause is fixed the entry
// has to go, so that the table does not keep a reason that is no longer true.
func TestTheZeroRoundTripExceptionsStillFail(t *testing.T) {
	for name, ex := range zeroRoundTripExceptions {
		t.Run(name, func(t *testing.T) {
			if ex.reason == "" {
				t.Fatal("an exception needs a reason")
			}
			text, err := json.Marshal(ex.value)
			if err != nil {
				return // does not encode: the reason holds
			}
			into := reflect.New(reflect.TypeOf(ex.value))
			if err := json.Unmarshal(text, into.Interface()); err == nil && reflect.DeepEqual(into.Elem().Interface(), ex.value) {
				t.Errorf("the zero %s round trips (%s): remove it from zeroRoundTripExceptions", name, text)
			}
		})
	}
	if _, err := json.Marshal(Operation{}); err == nil {
		t.Error("Operation{} was encoded")
	}
}

// guarantee: Speed.UnmarshalText
//
// UnmarshalText reads the empty text (and no text) as the zero Speed, whatever the value held before,
// and still reads every name it read before and refuses every other text with an error that matches
// ErrInvalid; "unknown(N)", which the library produces for a speed code it does not know, is
// refused today, and this row documents that state.
func TestSpeedUnmarshalTextReadsTheEmptyTextAsTheZeroValue(t *testing.T) {
	tests := []struct {
		name    string
		before  Speed
		text    []byte
		want    Speed
		wantErr bool
	}{
		{"the empty text", SpeedAuto, []byte{}, "", false},
		{"no text", Speed1000Full, nil, "", false},
		{"the empty text on the zero value", "", []byte{}, "", false},
		{"auto", "", []byte("auto"), SpeedAuto, false},
		{"10Mbps-half", "", []byte("10Mbps-half"), Speed10Half, false},
		{"10Mbps-full", "", []byte("10Mbps-full"), Speed10Full, false},
		{"100Mbps-half", "", []byte("100Mbps-half"), Speed100Half, false},
		{"100Mbps-full", "", []byte("100Mbps-full"), Speed100Full, false},
		{"1000Mbps-full", "", []byte("1000Mbps-full"), Speed1000Full, false},
		{"link-down, which a reading reports", "", []byte("link-down"), SpeedLinkDown, false},
		{"an unknown name", SpeedAuto, []byte("warp"), SpeedAuto, true},
		{"a name with a space", SpeedAuto, []byte(" auto"), SpeedAuto, true},
		{"a name in capitals", SpeedAuto, []byte("AUTO"), SpeedAuto, true},
		{"a text of a space", SpeedAuto, []byte(" "), SpeedAuto, true},
		{"a VLAN mode name", SpeedAuto, []byte("none"), SpeedAuto, true},
		{"unknown(N), which the library produces and the decoder refuses", SpeedAuto, []byte("unknown(7)"), SpeedAuto, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.before
			err := got.UnmarshalText(tc.text)
			if (err != nil) != tc.wantErr || (err != nil && !errors.Is(err, ErrInvalid)) {
				t.Fatalf("UnmarshalText(%q): error %v, want error %v", tc.text, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("UnmarshalText(%q) left %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// guarantee: VLANMode.UnmarshalText
//
// UnmarshalText reads the empty text (and no text) as the zero VLANMode, whatever the value held
// before, and still reads every name it read before and refuses every other text with an error that
// matches ErrInvalid.
func TestVLANModeUnmarshalTextReadsTheEmptyTextAsTheZeroValue(t *testing.T) {
	tests := []struct {
		name    string
		before  VLANMode
		text    []byte
		want    VLANMode
		wantErr bool
	}{
		{"the empty text", VLANModeDot1Q, []byte{}, "", false},
		{"no text", VLANModeMTU, nil, "", false},
		{"the empty text on the zero value", "", []byte{}, "", false},
		{"mtu", "", []byte("mtu"), VLANModeMTU, false},
		{"port-based", "", []byte("port-based"), VLANModePortBased, false},
		{"802.1q", "", []byte("802.1q"), VLANModeDot1Q, false},
		{"none, which a reading reports", "", []byte("none"), VLANModeNone, false},
		{"an unknown name", VLANModeMTU, []byte("vxlan"), VLANModeMTU, true},
		{"a name with a space", VLANModeMTU, []byte("mtu "), VLANModeMTU, true},
		{"a name in capitals", VLANModeMTU, []byte("MTU"), VLANModeMTU, true},
		{"a text of a space", VLANModeMTU, []byte(" "), VLANModeMTU, true},
		{"a speed name", VLANModeMTU, []byte("auto"), VLANModeMTU, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.before
			err := got.UnmarshalText(tc.text)
			if (err != nil) != tc.wantErr || (err != nil && !errors.Is(err, ErrInvalid)) {
				t.Fatalf("UnmarshalText(%q): error %v, want error %v", tc.text, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("UnmarshalText(%q) left %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// The empty text also reads through encoding/json: as a struct field (over a value that was set), as
// a map key, and an absent field leaves the value alone.
func TestTheEmptyTextOfASpeedAndAVLANModeReadsThroughJSON(t *testing.T) {
	type fields struct {
		S Speed
		M VLANMode
	}
	got := fields{S: SpeedAuto, M: VLANModeMTU}
	if err := json.Unmarshal([]byte(`{"S":"","M":""}`), &got); err != nil || got != (fields{}) {
		t.Errorf("struct fields: got %+v, %v, want the zero value", got, err)
	}
	got = fields{S: SpeedAuto, M: VLANModeMTU}
	if err := json.Unmarshal([]byte(`{}`), &got); err != nil || got != (fields{S: SpeedAuto, M: VLANModeMTU}) {
		t.Errorf("absent fields: got %+v, %v, want them unchanged", got, err)
	}
	var speeds map[Speed]int
	if err := json.Unmarshal([]byte(`{"":1,"auto":2}`), &speeds); err != nil || !reflect.DeepEqual(speeds, map[Speed]int{"": 1, SpeedAuto: 2}) {
		t.Errorf("Speed map key: got %v, %v", speeds, err)
	}
	var modes map[VLANMode]int
	if err := json.Unmarshal([]byte(`{"":1,"802.1q":2}`), &modes); err != nil || !reflect.DeepEqual(modes, map[VLANMode]int{"": 1, VLANModeDot1Q: 2}) {
		t.Errorf("VLANMode map key: got %v, %v", modes, err)
	}
	if err := json.Unmarshal([]byte(`{"S":"warp"}`), &got); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown speed: got %v, want ErrInvalid", err)
	}
	if err := json.Unmarshal([]byte(`{"M":"vxlan"}`), &got); !errors.Is(err, ErrInvalid) {
		t.Errorf("an unknown VLAN mode: got %v, want ErrInvalid", err)
	}
}

// guarantee: Speed.Validate
// guarantee: VLANMode.Validate
//
// The decoder reads the empty value, the writers still refuse it: a zero Speed or VLANMode is no
// setting. Only the decoder changed.
func TestTheZeroSpeedAndVLANModeAreStillRefusedAsSettings(t *testing.T) {
	runValidate(t, []validateCase{
		{"the zero Speed", Speed(""), true},
		{"the zero VLANMode", VLANMode(""), true},
		{"a PortConfig with the zero Speed", PortConfig{ID: 1}, true},
		{"a VLANModeSetting with the zero mode", VLANModeSetting{}, true},
		{"a PortConfig with a speed", PortConfig{ID: 1, Speed: SpeedAuto}, false},
		{"a VLANModeSetting with a mode", VLANModeSetting{Mode: VLANModePortBased}, false},
	})
}
