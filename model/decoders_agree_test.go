package model

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

// Defect class DC-L05 (one rule at every entry, escp-go#216): a scalar input type has one rule,
// its Validate, and its decoders (UnmarshalText, and UnmarshalJSON for the numbers) must apply it, so
// that decoding a file, a snapshot or a map key refuses what a writer would refuse. This file is the
// one test that compares the two for every scalar type, over the type's own values, around the
// edges of its limits, in every form a decoder is reached by (text, JSON string, JSON number, a
// struct field and a map key), and a scan that fails when a new scalar type with a Validate has
// neither a decoder in the table nor a stated exemption.

// reportedOnly is the one list of values a decoder reads although Validate refuses them: values that
// a switch reports and no caller can write. Each entry has its reason. The zero value of a type
// is the other allowance (decoderSubject.zeroReads), because a record that says "not set" is a
// reading too.
var reportedOnly = []struct{ typ, text, reason string }{
	{"Speed", string(SpeedLinkDown), "a port without a link reports link-down; no port can be set to it, so Validate refuses it as a setting"},
	{"VLANMode", string(VLANModeNone), "a switch with no VLAN mode in force reports none; it cannot be written, so Validate refuses it as a setting"},
}

func isReportedOnly(typ, text string) bool {
	return slices.ContainsFunc(reportedOnly, func(r struct{ typ, text, reason string }) bool {
		return r.typ == typ && r.text == text
	})
}

// decoderExemptions names the exported scalar types that have a Validate and no decoder, each with
// the reason. The records (structs) are not listed: a record gets no decoder of its own by design,
// the caller decodes it and then calls Validate before writing. It is empty today: every scalar
// type with a Validate has a decoder.
var decoderExemptions = map[string]string{}

// outcome is what one decoder did with one input.
type outcome struct {
	err      error
	value    string // the text of the receiver after the decode
	changed  bool   // the receiver is not what it held before
	validate error  // Validate of the receiver after the decode
	zero     bool   // the receiver is the zero value
}

func (o outcome) accepted() bool { return o.err == nil }

// decodeForm is one way to reach a decoder.
type decodeForm struct {
	name    string
	applies func(in string) bool
	read    func(in string) outcome
}

// universeEntry is one value of a type, in its canonical text, with what Validate says of it.
type universeEntry struct {
	text     string
	validate error
	zero     bool
}

// subjectOptions are the facts about a type that the table states and the test then holds the
// decoder to.
type subjectOptions struct {
	numeric bool // a JSON number reaches UnmarshalJSON
	// zeroReads: the text of the zero value is read although Validate refuses it ("not set").
	zeroReads bool
	// emptyIsZero: the empty text is read as the zero value.
	emptyIsZero bool
	// sameMessage: a refusal has Validate's own message (the other types refuse with their own
	// message, which still matches ErrInvalid).
	sameMessage bool
	// extra are spellings that a decoder must not take for a value, besides the generic ones.
	extra []string
}

// decoderSubject is one scalar type with a Validate and a decoder, seen from outside.
type decoderSubject struct {
	name     string
	opts     subjectOptions
	universe []universeEntry
	forms    []decodeForm
}

// accepts is whether a decoder may read the value: Validate accepts it, or it is the zero value
// of a type that reads it, or it is a reported-only value.
func (s decoderSubject) accepts(e universeEntry) bool {
	return e.validate == nil || (s.opts.zeroReads && e.zero) || isReportedOnly(s.name, e.text)
}

// readingAllowed is the same test for a value a decoder produced.
func (s decoderSubject) readingAllowed(o outcome) bool {
	return o.validate == nil || (s.opts.zeroReads && o.zero) || isReportedOnly(s.name, o.value)
}

// jsonQuoted is a JSON string holding in; in must be valid UTF-8.
func jsonQuoted(in string) string {
	b, err := json.Marshal(in)
	if err != nil {
		panic(err) // a string always encodes
	}
	return string(b)
}

// looksLikeNumber is whether in could be a JSON number literal (or a well-formed one that a decoder
// of a number must still refuse); a space around it is JSON's own, which a decoder never sees.
func looksLikeNumber(in string) bool {
	return in != "" && in == strings.TrimSpace(in) && json.Valid([]byte(in)) && (in[0] == '-' || (in[0] >= '0' && in[0] <= '9'))
}

// newDecoderSubject builds the subject of T: values are the values worth trying, text is the one
// canonical text of a value, and seed is what a receiver holds before a decode, which no decode of
// the candidates produces, so that "left alone" can be told from "set".
func newDecoderSubject[T interface {
	comparable
	Validate() error
}, P interface {
	*T
	encoding.TextUnmarshaler
}](name string, opts subjectOptions, seed T, values []T, text func(T) string) decoderSubject {
	var zero T
	describe := func(v T, err error) outcome {
		return outcome{err: err, value: text(v), changed: v != seed, validate: v.Validate(), zero: v == zero}
	}
	validUTF8 := utf8.ValidString
	forms := []decodeForm{
		{name: "text", applies: func(string) bool { return true }, read: func(in string) outcome {
			v := seed
			err := P(&v).UnmarshalText([]byte(in))
			return describe(v, err)
		}},
		{name: "JSON string", applies: validUTF8, read: func(in string) outcome {
			holder := struct{ V T }{V: seed}
			err := json.Unmarshal([]byte(`{"V":`+jsonQuoted(in)+`}`), &holder)
			return describe(holder.V, err)
		}},
		{name: "map key", applies: validUTF8, read: func(in string) outcome {
			var m map[T]int
			if err := json.Unmarshal([]byte(`{`+jsonQuoted(in)+`:1}`), &m); err != nil {
				return describe(seed, err)
			}
			for k := range m {
				return describe(k, nil)
			}
			return outcome{err: errors.New("the map has no key")}
		}},
	}
	if opts.numeric {
		forms = append(forms, decodeForm{name: "JSON number", applies: looksLikeNumber, read: func(in string) outcome {
			holder := struct{ V T }{V: seed}
			err := json.Unmarshal([]byte(`{"V":`+in+`}`), &holder)
			return describe(holder.V, err)
		}})
	}
	s := decoderSubject{name: name, opts: opts, forms: forms}
	for i, v := range values {
		if slices.Contains(values[:i], v) {
			continue
		}
		s.universe = append(s.universe, universeEntry{text: text(v), validate: v.Validate(), zero: v == zero})
	}
	return s
}

func numberText[T ~int](v T) string { return strconv.Itoa(int(v)) }

func stringText[T ~string](v T) string { return string(v) }

// numberSubject is the subject of a numeric type: its decimal text, zero read as "not set", and
// Validate's own message on a refusal.
func numberSubject[T interface {
	~int
	Validate() error
}, P interface {
	*T
	encoding.TextUnmarshaler
}](name string, values ...T) decoderSubject {
	return newDecoderSubject[T, P](name, subjectOptions{numeric: true, zeroReads: true, emptyIsZero: true, sameMessage: true},
		T(-1), values, numberText[T])
}

// decoderSubjects is every scalar type that has a Validate and a decoder, with the values around the
// edges of its limits.
func decoderSubjects() []decoderSubject {
	longHostname := strings.Repeat("a", MaxHostnameLen)
	longVLANName := strings.Repeat("a", MaxVLANNameLen)
	names := []Hostname{"", "a", "A", "0", "sw-1", "a-1-b", "a--b", "-a", "a-", "-", "a b", " a", "a ", "a.b", "a_b", "caf\u00e9",
		"a\x00b", "a\nb", Hostname(longHostname), Hostname(longHostname + "a"), "\u202ea"}
	vlanNames := []VLANName{"", "a", "A", "0", "sw-1", "a_b", "a-1-b", "-", "_", "a b", " a", "a ", "a.b", "caf\u00e9",
		"a\x00b", "a\nb", VLANName(longVLANName), VLANName(longVLANName + "a"), "\u202ea"}
	methods := []Method{"", "NoSuchMethod", "info", "INFO", " Info", "Info "}
	for _, s := range Settings() {
		methods = append(methods, s.Reads...)
		for _, w := range s.Writes {
			methods = append(methods, w.Name)
		}
	}
	return []decoderSubject{
		numberSubject[PortID]("PortID", -32, -1, 0, 1, 2, 8, MaxPort-1, MaxPort, MaxPort+1, 33, 255, 256),
		numberSubject[LAGID]("LAGID", -1, 0, 1, LAGGroupCount, LAGGroupCount+1, 255, 256),
		numberSubject[VLANID]("VLANID", -1, 0, MinVLANID, 2, 100, MaxVLANID-1, MaxVLANID, MaxVLANID+1, 4095, 65535, 65536),
		numberSubject[PortVLANID]("PortVLANID", -1, 0, 1, 2, 100, MaxPortVLANID-1, MaxPortVLANID, MaxPortVLANID+1, 256),
		numberSubject[Priority]("Priority", -1, 0, PriorityLowest, 2, 3, PriorityHighest, PriorityHighest+1, 9),
		numberSubject[Kbps]("Kbps", -RateStep, -1, 0, 1, RateStep-1, RateStep, RateStep+1, 2*RateStep, 100, MaxBandwidth-RateStep,
			MaxBandwidth, MaxBandwidth+1, MaxBandwidth+RateStep),
		newDecoderSubject[Hostname, *Hostname]("Hostname", subjectOptions{zeroReads: true, emptyIsZero: true, sameMessage: true},
			"seed-value", names, stringText[Hostname]),
		newDecoderSubject[VLANName, *VLANName]("VLANName", subjectOptions{zeroReads: true, emptyIsZero: true, sameMessage: true},
			"seed_value", vlanNames, stringText[VLANName]),
		newDecoderSubject[Speed, *Speed]("Speed", subjectOptions{zeroReads: true, emptyIsZero: true, extra: []string{"unknown(7)", "warp"}},
			"seed", append(append([]Speed{"", "auto ", "AUTO", "Auto"}, configurableSpeeds[:]...), SpeedLinkDown), stringText[Speed]),
		newDecoderSubject[VLANMode, *VLANMode]("VLANMode", subjectOptions{zeroReads: true, emptyIsZero: true, extra: []string{"warp"}},
			"seed", append(append([]VLANMode{"", "NONE", "None"}, VLANModes()...), VLANModeNone), stringText[VLANMode]),
		newDecoderSubject[QoSMode, *QoSMode]("QoSMode", subjectOptions{extra: []string{"0", "1", "2", "-1", "3"}},
			-1, []QoSMode{-1, 0, 1, 2, 3, 255}, QoSMode.String),
		newDecoderSubject[Impact, *Impact]("Impact", subjectOptions{extra: []string{"0", "1", "2", "3", "-1", "4"}},
			-1, []Impact{-1, 0, ImpactCosmetic, ImpactTraffic, ImpactManagement, ImpactManagement + 1, 255}, Impact.String),
		newDecoderSubject[Method, *Method]("Method", subjectOptions{}, "seed", methods, stringText[Method]),
	}
}

// hostileTexts are inputs that no scalar type may take for a value, whatever the type, or that must
// be told apart by their spelling.
func hostileTexts() []string {
	return []string{
		"", " ", "\t", "+", "-", "+3", "03", "-0", "-03", "3.0", "3e0", "1e1", "0x10", "1_0", " 3", "3 ", "\n3",
		"\uff13", "\u0663", "3\x00", "\x00", "a\x00b", "a\nb", "\u202e3", "null", "true", "{}", "[]", `"3"`,
		strings.Repeat("9", 100), "-" + strings.Repeat("9", 100), "9223372036854775808", "-9223372036854775809",
		strings.Repeat("a", 1<<20),
	}
}

// spellingsOf are the other ways to write a text: a sign, a leading zero, a fraction, an exponent, a
// space or a NUL on either side, upper and lower case.
func spellingsOf(text string) []string {
	return []string{"+" + text, "0" + text, text + ".0", text + "e0", " " + text, text + " ", text + "\x00", "\x00" + text,
		strings.ToUpper(text), strings.ToLower(text)}
}

// candidates is every input tried against a subject, once each.
func (s decoderSubject) candidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(in string) {
		if !seen[in] {
			seen[in] = true
			out = append(out, in)
		}
	}
	for _, e := range s.universe {
		add(e.text)
		for _, sp := range spellingsOf(e.text) {
			add(sp)
		}
	}
	for _, in := range hostileTexts() {
		add(in)
	}
	for _, in := range s.opts.extra {
		add(in)
	}
	return out
}

// shorten keeps the name of a subtest short for a megabyte of input.
func shorten(in string) string {
	if len(in) > 24 {
		return fmt.Sprintf("%q... (%d bytes)", in[:24], len(in))
	}
	return strconv.Quote(in)
}

// guarantee: PortID.UnmarshalText
// guarantee: PortID.UnmarshalJSON
// guarantee: LAGID.UnmarshalText
// guarantee: LAGID.UnmarshalJSON
// guarantee: VLANID.UnmarshalText
// guarantee: VLANID.UnmarshalJSON
// guarantee: PortVLANID.UnmarshalText
// guarantee: PortVLANID.UnmarshalJSON
// guarantee: Priority.UnmarshalText
// guarantee: Priority.UnmarshalJSON
// guarantee: Kbps.UnmarshalText
// guarantee: Kbps.UnmarshalJSON
// guarantee: Hostname.UnmarshalText
// guarantee: VLANName.UnmarshalText
// guarantee: Speed.UnmarshalText
// guarantee: VLANMode.UnmarshalText
//
// Every scalar decoder reads a value exactly when its type's Validate accepts it, or it is the zero
// value ("not set"), or it is a reported-only value (reportedOnly); the same in every form a decoder
// is reached by. What it refuses it refuses with an error that matches ErrInvalid, with Validate's own
// message for the numbers and the names, and leaves the receiver alone.
func TestDecodersAgreeWithValidate(t *testing.T) {
	for _, s := range decoderSubjects() {
		for _, e := range s.universe {
			t.Run(s.name+"/"+shorten(e.text), func(t *testing.T) {
				want := s.accepts(e)
				for _, f := range s.forms {
					if !f.applies(e.text) {
						continue
					}
					got := f.read(e.text)
					if got.accepted() != want {
						t.Errorf("%s: decoding %q: accepted=%v (%v), Validate says %v and the allowances say %v",
							f.name, e.text, got.accepted(), got.err, e.validate, want)
						continue
					}
					if want {
						if got.value != e.text {
							t.Errorf("%s: decoding %q read %q", f.name, e.text, got.value)
						}
						continue
					}
					assertRefusal(t, f.name, e.text, got)
					if s.opts.sameMessage && got.err != nil && got.err.Error() != e.validate.Error() {
						t.Errorf("%s: decoding %q is refused with %q, Validate says %q", f.name, e.text, got.err, e.validate)
					}
				}
			})
		}
	}
}

// assertRefusal holds a refusal to the library's rule: ErrInvalid, and the receiver not touched.
func assertRefusal(t *testing.T, form, in string, got outcome) {
	t.Helper()
	if !errors.Is(got.err, ErrInvalid) {
		t.Errorf("%s: the refusal of %s does not match ErrInvalid: %v", form, shorten(in), got.err)
	}
	if got.changed {
		t.Errorf("%s: the receiver was changed to %q by the refused %s", form, got.value, shorten(in))
	}
}

// Every other spelling is refused, and what is read at all is read the same in every form: a decoder
// reads a text only in the one spelling that names a value (the text of the value it reads), the
// empty text is the zero value or a refusal as the type says, and whatever it reads passes the same
// rule as in TestDecodersAgreeWithValidate.
func TestDecodersRefuseEveryOtherSpelling(t *testing.T) {
	for _, s := range decoderSubjects() {
		for _, in := range s.candidates() {
			t.Run(s.name+"/"+shorten(in), func(t *testing.T) {
				// subtests: the forms of one input are one case; the form is in every message.
				first := s.forms[0].read(in)
				for _, f := range s.forms {
					if !f.applies(in) {
						continue
					}
					got := f.read(in)
					if got.accepted() != first.accepted() || got.value != first.value {
						t.Errorf("the %s form reads %s as (%v, %q) and the text form as (%v, %q)", f.name, shorten(in),
							got.accepted(), got.value, first.accepted(), first.value)
					}
					if !got.accepted() {
						assertRefusal(t, f.name, in, got)
						continue
					}
					checkReading(t, s, f.name, in, got)
				}
			})
		}
	}
}

// checkReading holds an accepted input to the rules: it was in the spelling of the value it read (or
// empty and the zero value, when the type says an empty text is that), and the value is one the
// allowances let a decoder read.
func checkReading(t *testing.T, s decoderSubject, form, in string, got outcome) {
	t.Helper()
	switch {
	case in == "":
		if !s.opts.emptyIsZero || !got.zero {
			t.Errorf("%s: the empty text read as %q (zero %v), the table says emptyIsZero is %v", form, got.value, got.zero, s.opts.emptyIsZero)
		}
	case got.value != in:
		t.Errorf("%s: %s was read as %q, which is another spelling of it", form, shorten(in), got.value)
	}
	if !s.readingAllowed(got) {
		t.Errorf("%s: %s was read as %q, which Validate refuses (%v) and no allowance covers", form, shorten(in), got.value, got.validate)
	}
}

// The empty text is a refusal or the zero value, as each type says, in every form.
func TestTheEmptyTextIsTheZeroValueOnlyWhereTheTableSaysSo(t *testing.T) {
	for _, s := range decoderSubjects() {
		t.Run(s.name, func(t *testing.T) {
			// subtests: the forms of one type are one case; the form is in every message.
			for _, f := range s.forms {
				if !f.applies("") {
					continue
				}
				got := f.read("")
				if got.accepted() != s.opts.emptyIsZero {
					t.Errorf("%s: the empty text: accepted=%v (%v), the table says emptyIsZero=%v", f.name, got.accepted(), got.err, s.opts.emptyIsZero)
				}
			}
		})
	}
}

// Every reported-only entry is still what it says: Validate refuses the value and the decoder reads
// it, and it names a type of the table.
func TestTheReportedOnlyValuesAreStillRefusedByValidateAndReadByTheDecoder(t *testing.T) {
	subjects := map[string]decoderSubject{}
	for _, s := range decoderSubjects() {
		subjects[s.name] = s
	}
	for _, r := range reportedOnly {
		t.Run(r.typ+"/"+r.text, func(t *testing.T) {
			s, ok := subjects[r.typ]
			if !ok {
				t.Fatalf("no subject %s", r.typ)
			}
			if r.reason == "" {
				t.Error("an allowance needs a reason")
			}
			i := slices.IndexFunc(s.universe, func(e universeEntry) bool { return e.text == r.text })
			if i < 0 {
				t.Fatalf("%q is not one of the values tried for %s", r.text, r.typ)
			}
			if s.universe[i].validate == nil {
				t.Errorf("Validate accepts %q now: remove it from reportedOnly", r.text)
			}
			if got := s.forms[0].read(r.text); !got.accepted() || got.value != r.text {
				t.Errorf("the decoder does not read %q: %v", r.text, got.err)
			}
		})
	}
}

// Every type tries its zero value, whichever way it reads it: the allowance for the zero value is
// then held to the decoder by TestDecodersAgreeWithValidate.
func TestEveryTypeTriesItsZeroValue(t *testing.T) {
	for _, s := range decoderSubjects() {
		t.Run(s.name, func(t *testing.T) {
			if !slices.ContainsFunc(s.universe, func(e universeEntry) bool { return e.zero }) {
				t.Error("the values tried do not include the zero value")
			}
		})
	}
}

// ----------------------------------------------------------------------------------------------
// The scan: a scalar input type with a Validate has a decoder in the table above, or a stated
// exemption.

// typeFacts is what the scan learns of one declared type.
type typeFacts struct {
	scalar  bool // not a struct, an interface or a func: a record gets no decoder by design
	methods map[string]bool
}

// scalarFacts reads the exported types and their methods of the packages in dirs of fsys, outside
// the tests, keyed by "package.Type".
func scalarFacts(fsys fs.FS, dirs []string) (map[string]*typeFacts, error) {
	facts := map[string]*typeFacts{}
	get := func(key string) *typeFacts {
		if facts[key] == nil {
			facts[key] = &typeFacts{methods: map[string]bool{}}
		}
		return facts[key]
	}
	fset := token.NewFileSet()
	for _, dir := range dirs {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := fs.ReadFile(fsys, path.Join(dir, name))
			if err != nil {
				return nil, err
			}
			file, err := parser.ParseFile(fset, name, src, 0)
			if err != nil {
				return nil, err
			}
			recordDeclarations(file, get)
		}
	}
	return facts, nil
}

// recordDeclarations adds the exported types and the methods of one file to the facts.
func recordDeclarations(file *ast.File, get func(key string) *typeFacts) {
	pkg := file.Name.Name
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
					get(pkg + "." + ts.Name.Name).scalar = isScalarType(ts.Type)
				}
			}
		case *ast.FuncDecl:
			if recv := receiverName(d); recv != "" {
				get(pkg + "." + recv).methods[d.Name.Name] = true
			}
		}
	}
}

// isScalarType is false for a struct, an interface and a func type.
func isScalarType(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.StructType, *ast.InterfaceType, *ast.FuncType:
		return false
	}
	return true
}

// receiverName is the type a method is declared on, without the pointer, or "" for a function.
func receiverName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) != 1 {
		return ""
	}
	expr := d.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// decoderGaps lists what is wrong between the scalar types with a Validate in dirs of fsys, the types of
// the agreement table (subjects, as "package.Type") and the exemptions (type to reason): a type with
// a Validate that has no decoder and no exemption, a decoder that is not in the table, an entry of
// the table or of the exemptions that names no such type or is stale.
func decoderGaps(fsys fs.FS, dirs []string, subjects map[string]bool, exemptions map[string]string) ([]string, error) {
	facts, err := scalarFacts(fsys, dirs)
	if err != nil {
		return nil, err
	}
	var gaps []string
	validated := map[string]bool{}
	for key, f := range facts {
		if !f.scalar || !f.methods["Validate"] {
			continue
		}
		validated[key] = true
		gaps = append(gaps, checkScalar(key, f.methods["UnmarshalText"], subjects[key], exemptions)...)
	}
	for key := range subjects {
		if !validated[key] {
			gaps = append(gaps, key+" is in the agreement table but is not an exported scalar type with a Validate")
		}
	}
	for key := range exemptions {
		if !validated[key] {
			gaps = append(gaps, key+" is exempted but is not an exported scalar type with a Validate")
		}
	}
	sort.Strings(gaps)
	return gaps, nil
}

// checkScalar is the verdict on one scalar type with a Validate.
func checkScalar(key string, hasDecoder, inTable bool, exemptions map[string]string) []string {
	reason, exempt := exemptions[key]
	switch {
	case exempt && reason == "":
		return []string{key + " is exempted without a reason"}
	case exempt && (hasDecoder || inTable):
		return []string{key + " is exempted but has a decoder: remove the exemption and give it a row in the table"}
	case exempt:
		return nil
	case !hasDecoder:
		return []string{key + " has a Validate and no UnmarshalText: give it a decoder with the rule of Validate and a row in decoderSubjects, or exempt it in decoderExemptions with a reason"}
	case !inTable:
		return []string{key + " has a decoder and no row in decoderSubjects: add it, so that the decoder is held to Validate"}
	}
	return nil
}

// The agreement table is the list of the scalar types that have a Validate and a decoder: it has one
// row for each, and an exported scalar type with a Validate that has neither a decoder nor an exemption
// fails here.
func TestEveryScalarTypeWithAValidateHasADecoderInTheTableOrAnExemption(t *testing.T) {
	subjects := map[string]bool{}
	for _, s := range decoderSubjects() {
		subjects["model."+s.name] = true
	}
	// The packages of the exported surface, from the repository root.
	gaps, err := decoderGaps(os.DirFS(".."), []string{".", "model", "safety", "escptest"}, subjects, decoderExemptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gaps {
		t.Error(g)
	}
	if len(subjects) < 13 {
		t.Errorf("the table has %d types, it had 13: the scan or the table is broken", len(subjects))
	}
}

// The scan itself, run on source it is given: each way a type can be wrong is found, and each way it
// can be right is not reported.
func TestTheScanOfDecodersFindsWhatIsWrong(t *testing.T) {
	const decl = "package model\n"
	tests := []struct {
		name       string
		files      map[string]string
		subjects   []string
		exemptions map[string]string
		want       []string // fragments, one per gap
	}{
		{"a scalar with a Validate and a decoder in the table", map[string]string{"a.go": decl +
			"type T int\nfunc (T) Validate() error { return nil }\nfunc (*T) UnmarshalText([]byte) error { return nil }\n"},
			[]string{"model.T"}, nil, nil},
		{"a scalar with a Validate and no decoder", map[string]string{"a.go": decl +
			"type T int\nfunc (T) Validate() error { return nil }\n"},
			nil, nil, []string{"model.T has a Validate and no UnmarshalText"}},
		{"a string type with a pointer-receiver Validate and no decoder", map[string]string{"a.go": decl +
			"type T string\nfunc (*T) Validate() error { return nil }\n"},
			nil, nil, []string{"model.T has a Validate and no UnmarshalText"}},
		{"the method in another file than the type", map[string]string{"a.go": decl + "type T int\n", "b.go": decl +
			"func (T) Validate() error { return nil }\n"},
			nil, nil, []string{"model.T has a Validate and no UnmarshalText"}},
		{"a scalar with a decoder that is not in the table", map[string]string{"a.go": decl +
			"type T int\nfunc (T) Validate() error { return nil }\nfunc (*T) UnmarshalText([]byte) error { return nil }\n"},
			nil, nil, []string{"model.T has a decoder and no row"}},
		{"a scalar with no decoder and an exemption", map[string]string{"a.go": decl +
			"type T int\nfunc (T) Validate() error { return nil }\n"},
			nil, map[string]string{"model.T": "it is a name only"}, nil},
		{"an exemption without a reason", map[string]string{"a.go": decl +
			"type T int\nfunc (T) Validate() error { return nil }\n"},
			nil, map[string]string{"model.T": ""}, []string{"model.T is exempted without a reason"}},
		{"an exemption of a type that has a decoder now", map[string]string{"a.go": decl +
			"type T int\nfunc (T) Validate() error { return nil }\nfunc (*T) UnmarshalText([]byte) error { return nil }\n"},
			[]string{"model.T"}, map[string]string{"model.T": "old"}, []string{"model.T is exempted but has a decoder"}},
		{"a record with a Validate and no decoder is exempt by design", map[string]string{"a.go": decl +
			"type R struct{ A int }\nfunc (R) Validate() error { return nil }\n"},
			nil, nil, nil},
		{"an unexported scalar is not an input", map[string]string{"a.go": decl +
			"type t int\nfunc (t) Validate() error { return nil }\n"},
			nil, nil, nil},
		{"a scalar without a Validate is not checked", map[string]string{"a.go": decl + "type T int\n"},
			nil, nil, nil},
		{"a table row for a type that is gone", map[string]string{"a.go": decl + "type T int\n"},
			[]string{"model.Gone"}, nil, []string{"model.Gone is in the agreement table but"}},
		{"an exemption for a type that is gone", map[string]string{"a.go": decl + "type T int\n"},
			nil, map[string]string{"model.Gone": "x"}, []string{"model.Gone is exempted but"}},
		{"a test file is not read", map[string]string{"a_test.go": decl +
			"type T int\nfunc (T) Validate() error { return nil }\n"},
			nil, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for name, src := range tc.files {
				fsys["model/"+name] = &fstest.MapFile{Data: []byte(src)}
			}
			subjects := map[string]bool{}
			for _, key := range tc.subjects {
				subjects[key] = true
			}
			gaps, err := decoderGaps(fsys, []string{"model"}, subjects, tc.exemptions)
			if err != nil {
				t.Fatal(err)
			}
			if len(gaps) != len(tc.want) {
				t.Fatalf("got %d gaps %q, want %d %q", len(gaps), gaps, len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if !strings.Contains(gaps[i], want) {
					t.Errorf("gap %d is %q, want it to contain %q", i, gaps[i], want)
				}
			}
		})
	}
}

func TestTheScanOfDecodersReportsASourceItCannotRead(t *testing.T) {
	tests := []struct {
		name string
		fsys fs.FS
		dirs []string
	}{
		{"a directory that is not there", fstest.MapFS{}, []string{"model"}},
		{"source that does not parse", fstest.MapFS{"model/a.go": &fstest.MapFile{Data: []byte("package model\nfunc (")}}, []string{"model"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decoderGaps(tc.fsys, tc.dirs, nil, nil); err == nil {
				t.Error("no error")
			}
		})
	}
}
