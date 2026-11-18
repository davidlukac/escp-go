package readiness

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The oracle: GitHub's own renderer is the ground truth of what a reader sees. Each description in
// testdata/oracle/NN-name.md has the HTML that GitHub rendered for it next to it (NN-name.html,
// recorded with `make oracle-refresh`, which needs `gh auth` and is not run in CI). The test reads
// from the recorded HTML which task-list checkboxes of the "Merge readiness" section are unchecked and
// holds Judge to two promises: it reports at least as many unticked boxes as the renderer shows, and it
// is red whenever the renderer shows one. Where Judge is stricter than the renderer, the expectation
// below says so.
var (
	headingTag    = regexp.MustCompile(`(?s)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	tagText       = regexp.MustCompile(`<[^>]*>`)
	checkboxTag   = regexp.MustCompile(`<input[^>]*type="checkbox"[^>]*>`)
	checkedAttr   = regexp.MustCompile(`\schecked[\s=>]`)
	renderedTitle = regexp.MustCompile(`^(?i)merge\s+readiness$`)
)

// rendered is what the recorded HTML shows of the section: whether the section exists, and how many
// checkboxes after its heading, up to the next heading of level 1 or 2, are unchecked.
func rendered(html string) (found bool, unchecked int) {
	headings := headingTag.FindAllStringSubmatchIndex(html, -1)
	for i, h := range headings {
		title := strings.TrimSpace(tagText.ReplaceAllString(html[h[4]:h[5]], ""))
		if !renderedTitle.MatchString(title) {
			continue
		}
		found = true
		end := len(html)
		for _, next := range headings[i+1:] {
			if level := html[next[2]:next[3]]; level == "1" || level == "2" {
				end = next[0]
				break
			}
		}
		for _, box := range checkboxTag.FindAllString(html[h[1]:end], -1) {
			if !checkedAttr.MatchString(box) {
				unchecked++
			}
		}
	}
	return found, unchecked
}

// oracleCases says what Judge must answer for each description: pass, and the reason when it is
// stricter than the renderer.
var oracleCases = map[string]struct {
	pass bool
	note string
}{
	"01-template-unticked":                     {false, ""},
	"02-template-ticked":                       {true, ""},
	"03-no-section":                            {true, ""},
	"04-inline-code-comment-marker":            {false, ""},
	"05-unclosed-inline-comment-marker":        {false, ""},
	"06-comment-then-fence-on-one-line":        {false, ""},
	"07-fence-in-list-item":                    {false, ""},
	"08-fence-line-in-html-block":              {false, ""},
	"09-ordered-box":                           {false, ""},
	"10-ordered-box-with-parenthesis":          {false, ""},
	"11-nested-list-box":                       {false, ""},
	"12-quoted-box":                            {false, ""},
	"13-quoted-heading":                        {false, ""},
	"14-no-break-space-in-box":                 {false, ""},
	"15-two-spaces-in-title":                   {false, ""},
	"16-html-block-heading-without-blank-line": {false, "the renderer shows raw HTML, Judge reads a heading"},
	"17-box-in-html-block":                     {false, "the renderer shows raw HTML, Judge reads a box"},
	"18-indented-code-box":                     {false, "the renderer shows code, Judge reads a box"},
	"19-setext-heading-ends-section":           {false, "the renderer ends the section at the underlined heading"},
	"20-tilde-fence-quoting-the-section":       {false, "the renderer shows code, Judge reads a quoted heading"},
	"21-long-fence-quoting-the-section":        {false, "the renderer shows code, Judge reads a quoted heading"},
	"22-tab-after-marker":                      {false, ""},
	"23-star-marker":                           {false, ""},
	"24-empty-box":                             {false, "the renderer shows no task for an empty box, Judge reports it"},
	"25-text-glued-to-box":                     {false, "the renderer shows no task, Judge reports the shape"},
	"26-escaped-comment-marker":                {false, ""},
	"27-unclosed-fence-in-quote":               {false, ""},
	"28-fence-indented-four-in-list":           {false, ""},
	"29-comment-box-on-one-line":               {true, ""},
	"30-comment-box-over-lines":                {false, "the renderer hides the comment, Judge reports a box in it"},
	"31-level-3-heading":                       {false, "Judge reads the section at any level"},
	"32-marker-without-space":                  {false, "the section has no box: a markup the renderer shows as text"},
	"33-section-without-boxes":                 {false, ""},
	"34-other-section-after":                   {true, ""},
}

func TestJudgeIsAtLeastAsStrictAsTheRenderer(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "oracle", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no oracle descriptions: %v", err)
	}
	if len(files) != len(oracleCases) {
		t.Errorf("%d descriptions in testdata/oracle, %d expectations", len(files), len(oracleCases))
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".md")
		t.Run(name, func(t *testing.T) {
			want, ok := oracleCases[name]
			if !ok {
				t.Fatal("no expectation for this description")
			}
			body, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			html, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".html")
			if err != nil {
				t.Fatalf("the rendering is not recorded (make oracle-refresh): %v", err)
			}
			_, unchecked := rendered(string(html))
			got := Judge(string(body))
			if got.Pass != want.pass {
				t.Errorf("pass = %v, want %v (%s): %q", got.Pass, want.pass, want.note, got.Lines)
			}
			if unchecked > 0 && got.Pass {
				t.Errorf("green, but the renderer shows %d unchecked box(es)", unchecked)
			}
			if got.Unticked < unchecked {
				t.Errorf("reports %d unticked, the renderer shows %d", got.Unticked, unchecked)
			}
		})
	}
}

func TestTheRecordedHTMLIsReadRight(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		found     bool
		unchecked int
	}{
		{"two unchecked and one checked", `<h2>Merge readiness</h2><li><input type="checkbox" id="" disabled="" class="x"> a</li>
<li><input type="checkbox" id="" disabled="" checked="" class="x"> b</li><li><input type="checkbox" disabled=""> c</li>`, true, 2},
		{"a checked attribute at the end", `<h2>merge  readiness</h2><input type="checkbox" checked>`, true, 0},
		{"a heading with markup in it", `<h2><a id="x"></a>Merge readiness</h2><input type="checkbox">`, true, 1},
		{"another heading of level 2 ends the section", `<h2>Merge readiness</h2><input type="checkbox"><h2>Notes</h2><input type="checkbox">`, true, 1},
		{"a heading of level 3 does not", `<h2>Merge readiness</h2><input type="checkbox"><h3>Pending</h3><input type="checkbox">`, true, 2},
		{"boxes before the heading", `<input type="checkbox"><h2>Merge readiness</h2>`, true, 0},
		{"another section", `<h2>Notes</h2><input type="checkbox">`, false, 0},
		{"no heading", `<p>text</p>`, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if found, unchecked := rendered(tt.html); found != tt.found || unchecked != tt.unchecked {
				t.Errorf("rendered = %v, %d; want %v, %d", found, unchecked, tt.found, tt.unchecked)
			}
		})
	}
}
