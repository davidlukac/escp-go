package escp

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/model"
)

const (
	// defaultsSnapshotFile holds what a zero Config and the package's own limits resolve to.
	defaultsSnapshotFile = "testdata/defaults.txt"
	// updateDefaultsEnv, set to a non-empty value, rewrites the snapshot instead of comparing with it.
	updateDefaultsEnv = "UPDATE_DEFAULTS"
	// defaultsSnapshotHeader opens the file, so a reader of the diff knows what a change means.
	defaultsSnapshotHeader = "# What a zero Config resolves to in Dial, and the package limits and budgets a caller can run into.\n" +
		"# A change here is a change of behavior for every caller that left the field out: the pull request names its consumers\n" +
		"# (defect class DC-C03 of docs/defect-classes.md). Update with: UPDATE_DEFAULTS=1 go test -run TestTheConfigDefaultsAreTheSnapshot\n"
)

// durationUnits are the selectors of the time package that write a duration out as a literal.
var durationUnits = []string{"Nanosecond", "Microsecond", "Millisecond", "Second", "Minute", "Hour"}

// inlineDurationHolders are the functions of the package that hold a duration literal inside their
// body, and the snapshot line that shows what it resolves to: dial sets Config.Timeout to three
// seconds when it is zero.
var inlineDurationHolders = map[string]string{"dial": "Timeout"}

// packageLimits are the package-level values a caller meets as a limit, a timeout or a budget:
// what each is, in the order the snapshot lists them. A new duration constant that is missing here
// fails the completeness check below.
func packageLimits() []struct {
	name  string
	value any
} {
	return []struct {
		name  string
		value any
	}{
		{"NoRetries", NoRetries},
		{"MaxRetries", MaxRetries},
		{"MaxTimeout", MaxTimeout},
		{"defaultDiscoverWindow", defaultDiscoverWindow},
		{"defaultFirstContactBudget", defaultFirstContactBudget},
		{"leaseWait", leaseWait},
		{"ipMoveWait", ipMoveWait},
		{"ipMovePoll", ipMovePoll},
		{"settleBudget", settleBudget},
		{"drainWait", drainWait},
	}
}

// renderConfigDefaults writes what Dial makes of cfg: every field of the resolved Config but the
// credentials and the address, which are inputs, not defaults.
func renderConfigDefaults(t *testing.T, b *strings.Builder, name string, cfg Config) {
	t.Helper()
	c, err := dial(context.Background(), net.DefaultResolver, cfg)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	t.Cleanup(func() { c.Close() })
	fmt.Fprintf(b, "\n[%s]\n", name)
	v := reflect.ValueOf(c.cfg)
	for i := range v.NumField() {
		switch field := v.Type().Field(i).Name; field {
		case "Host", "MAC", "Password":
			continue
		default:
			switch x := v.Field(i); x.Kind() {
			case reflect.Func, reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
				fmt.Fprintf(b, "%s nil=%v\n", field, x.IsNil())
			case reflect.String:
				fmt.Fprintf(b, "%s %q\n", field, x.String())
			default:
				fmt.Fprintf(b, "%s %v\n", field, x.Interface())
			}
		}
	}
	fmt.Fprintf(b, "first-contact-budget %v\n", c.firstContactBudget())
	// the registry's default as a program sees it: TestMain turns the claim off for this package
	fmt.Fprintf(b, "claims-its-switch %v\n", claimsInAProgram)
}

// renderDefaults is the whole snapshot.
func renderDefaults(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(defaultsSnapshotHeader)
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"Config with a Host", Config{Host: "192.0.2.1"}},
		{"Config with a Host and NoRetries", Config{Host: "192.0.2.1", Retries: NoRetries}},
		{"Config with a MAC and no Host", Config{MAC: model.MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01}}},
	} { // subtests: one document is rendered, each case adds a section to it
		renderConfigDefaults(t, &b, tc.name, tc.cfg)
	}
	b.WriteString("\n[package limits and budgets]\n")
	for _, l := range packageLimits() { // subtests: one document is rendered, each limit adds a line to it
		fmt.Fprintf(&b, "%s %v\n", l.name, l.value)
	}
	return b.String()
}

// timeSelectors are the positions in a file where an expression names a duration unit of the time
// package (time.Second): the declaration name or the function that holds it, per position.
func timeSelectors(fset *token.FileSet, file *ast.File) (decls, funcs map[string]token.Position) {
	decls, funcs = map[string]token.Position{}, map[string]token.Position{}
	uses := func(n ast.Node) (token.Pos, bool) {
		var at token.Pos
		ast.Inspect(n, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && !at.IsValid() {
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && slices.Contains(durationUnits, sel.Sel.Name) {
					at = sel.Pos()
				}
			}
			return true
		})
		return at, at.IsValid()
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if v, ok := s.(*ast.ValueSpec); ok {
					if at, found := uses(v); found {
						decls[v.Names[0].Name] = fset.Position(at)
					}
				}
			}
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			if at, found := uses(d.Body); found {
				funcs[d.Name.Name] = fset.Position(at)
			}
		}
	}
	return decls, funcs
}

// shownNames is the first word of every line of the snapshot: the names it shows a value for.
func shownNames(snapshot string) map[string]bool {
	shown := map[string]bool{}
	for _, line := range strings.Split(snapshot, "\n") {
		if name, _, found := strings.Cut(line, " "); found {
			shown[name] = true
		}
	}
	return shown
}

// unlistedIn reports the durations of one file that the snapshot (its shownNames) does not show: a
// package-level duration that is not in it, and a duration literal inside a function that is not a
// known holder (a default written inline, such as dial's three seconds, is seen only through what
// the code makes of it, so a new one needs a line in the snapshot).
func unlistedIn(fset *token.FileSet, file *ast.File, shown map[string]bool) []string {
	var unlisted []string
	decls, funcs := timeSelectors(fset, file)
	for decl, at := range decls {
		if !shown[decl] {
			unlisted = append(unlisted, fmt.Sprintf("%s: the duration %s is not in %s: add it to packageLimits", at, decl, defaultsSnapshotFile))
		}
	}
	for fn, at := range funcs {
		if line, known := inlineDurationHolders[fn]; !known || !shown[line] {
			unlisted = append(unlisted, fmt.Sprintf("%s: %s holds a duration literal that the snapshot does not show: render what it resolves to and add the function to inlineDurationHolders", at, fn))
		}
	}
	slices.Sort(unlisted)
	return unlisted
}

// unlistedDefaults is unlistedIn over the package's own files.
func unlistedDefaults(t *testing.T, snapshot string) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	shown := shownNames(snapshot)
	var unlisted []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		unlisted = append(unlisted, unlistedIn(fset, file, shown)...)
	}
	return unlisted
}

// sectionLines is the lines of a snapshot, each led by the section it is in, so a line that two
// sections share is told apart.
func sectionLines(text string) []string {
	var out []string
	section := ""
	for _, l := range strings.Split(text, "\n") {
		switch {
		case l == "" || strings.HasPrefix(l, "#"):
		case strings.HasPrefix(l, "["):
			section = l
		default:
			out = append(out, section+" "+l)
		}
	}
	return out
}

// lineDifference lists the lines that only one of the two texts has, with - for the committed
// snapshot and + for what the code renders now, so a failure reads like the diff of the update.
func lineDifference(want, got string) string {
	wantLines, gotLines := sectionLines(want), sectionLines(got)
	var out []string
	for _, l := range wantLines {
		if !slices.Contains(gotLines, l) {
			out = append(out, "- "+l)
		}
	}
	for _, l := range gotLines {
		if !slices.Contains(wantLines, l) {
			out = append(out, "+ "+l)
		}
	}
	return strings.Join(out, "\n")
}

// checkSnapshot compares got with the file at path, or, when update is set, writes it there.
func checkSnapshot(path, got string, update bool) error {
	if update {
		return os.WriteFile(path, []byte(got), 0o600)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%w: write the snapshot with %s=1 go test -run TestTheConfigDefaultsAreTheSnapshot", err, updateDefaultsEnv)
	}
	if string(want) != got {
		return fmt.Errorf("a default or a limit changed (- committed, + now). If that is meant, name the consumers in the pull request and run %s=1 go test -run TestTheConfigDefaultsAreTheSnapshot:\n%s",
			updateDefaultsEnv, lineDifference(string(want), got))
	}
	return nil
}

// A zero Config resolves to what testdata/defaults.txt says, and so do the package's limits and
// budgets: a default that flips (relocation by MAC, a retry count, a timeout, a port, whether a
// client claims its switch) shows in the diff of the pull request that flips it, so that every
// consumer that relied on the old value is named before the merge (DC-C03). The update is one
// deliberate step: UPDATE_DEFAULTS=1 go test -run TestTheConfigDefaultsAreTheSnapshot.
func TestTheConfigDefaultsAreTheSnapshot(t *testing.T) {
	got := renderDefaults(t)
	t.Run("every duration of the package is in the snapshot", func(t *testing.T) {
		for _, line := range unlistedDefaults(t, got) {
			t.Error(line)
		}
	})
	t.Run("the snapshot is the committed one", func(t *testing.T) {
		if err := checkSnapshot(defaultsSnapshotFile, got, os.Getenv(updateDefaultsEnv) != ""); err != nil {
			t.Fatal(err)
		}
	})
}

// The check says what changed, names the way to update, and updates only when asked.
func TestTheDefaultsSnapshotCheckReportsAndUpdates(t *testing.T) {
	const committed = "# header\n\n[a]\nTimeout 3s\nRetries 2\n\n[b]\nTimeout 3s\n"
	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "defaults.txt")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Run("the same text passes", func(t *testing.T) {
		if err := checkSnapshot(write(t, committed), committed, false); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a changed value is shown with its section, and only that one", func(t *testing.T) {
		err := checkSnapshot(write(t, committed), strings.Replace(committed, "Timeout 3s", "Timeout 4s", 1), false)
		if err == nil {
			t.Fatal("a changed default passed")
		}
		for _, want := range []string{"- [a] Timeout 3s", "+ [a] Timeout 4s", updateDefaultsEnv} { // subtests: three checks on one report
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the report lacks %q:\n%v", want, err)
			}
		}
		if strings.Contains(err.Error(), "[b]") {
			t.Errorf("the report names a section that did not change:\n%v", err)
		}
	})
	t.Run("a missing file fails and says how to write it", func(t *testing.T) {
		err := checkSnapshot(filepath.Join(t.TempDir(), "none.txt"), committed, false)
		if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), updateDefaultsEnv) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("update writes the new text and leaves a comparison for the next run", func(t *testing.T) {
		path := write(t, "old\n")
		if err := checkSnapshot(path, committed, true); err != nil {
			t.Fatal(err)
		}
		if err := checkSnapshot(path, committed, false); err != nil {
			t.Fatalf("the file written by the update does not match: %v", err)
		}
	})
	t.Run("update into a missing directory fails", func(t *testing.T) {
		if err := checkSnapshot(filepath.Join(t.TempDir(), "no", "dir", "defaults.txt"), committed, true); err == nil {
			t.Fatal("an update that could not write passed")
		}
	})
}

// The completeness check sees what it claims to see: a duration constant or an inline literal that
// the snapshot does not show is reported, and one it shows is not.
func TestTheDefaultsCheckSeesAnUnlistedDuration(t *testing.T) {
	const src = "package x\n\nimport \"time\"\n\nconst listed = time.Second\nconst unlisted = 2 * time.Minute\nconst plain = 3\n\n" +
		"func inline() time.Duration { return time.Hour }\nfunc dial() time.Duration { return 3 * time.Second }\nfunc none() int { return 1 }\nfunc declared()\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		shown map[string]bool
		want  []string // names the report must mention
	}{
		{"nothing shown", map[string]bool{}, []string{"listed", "unlisted", "inline", "dial"}},
		{"the constants shown", map[string]bool{"listed": true, "unlisted": true}, []string{"inline", "dial"}},
		{"everything shown", map[string]bool{"listed": true, "unlisted": true, "Timeout": true}, []string{"inline"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := strings.Join(unlistedIn(fset, file, tc.shown), "\n")
			for _, name := range []string{"listed", "unlisted", "inline", "dial", "plain", "none", "declared"} { // subtests: every name is checked against one report
				mentioned := strings.Contains(report, " "+name+" ")
				if mentioned != slices.Contains(tc.want, name) {
					t.Errorf("%s mentioned = %v, want %v:\n%s", name, mentioned, slices.Contains(tc.want, name), report)
				}
			}
		})
	}
}

// The rendering names the defaults a caller relies on, whatever the committed file says: the file
// can be regenerated from a rendering that lost one.
func TestTheRenderedDefaultsNameWhatACallerRelies(t *testing.T) {
	out := renderDefaults(t)
	for _, tc := range []struct {
		name string
		line string
	}{
		{"timeout default", "Timeout " + (3 * time.Second).String()},
		{"retries default", fmt.Sprintf("Retries %d", defaultRetries)},
		{"retries when NoRetries", "Retries 0"},
		{"relocation is off", "RelocateByMAC false"},
		{"switch port", fmt.Sprintf("SwitchPort %d", defaultSwitchPort)},
		{"host port", fmt.Sprintf("HostPort %d", defaultHostPort)},
		{"broadcast", "BroadcastAddr 255.255.255.255"},
		{"max timeout", "MaxTimeout " + MaxTimeout.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains("\n"+out, "\n"+tc.line+"\n") {
				t.Fatalf("the snapshot has no line %q:\n%s", tc.line, out)
			}
		})
	}
}
