package main

import (
	"strings"
	"testing"
)

func TestBlockElementsBecomeLinesAndHeadingsGetAMarkdownPrefix(t *testing.T) {
	page := `<html><body><h1>Package escp</h1><p>First <b>bold</b> text.</p><h2 id="x">Index</h2><ul><li>one</li><li>two</li></ul><h4>Deep</h4></body></html>`
	// every block starts and ends a line, so two blocks in a row leave a blank line between them
	want := "# Package escp\n\nFirst bold text.\n\n## Index\n\none\n\ntwo\n\nDeep\n"
	got := htmlToText(page)
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestScriptsStylesNavigationAndGraphicsAreDroppedWithWhatIsInsideThem(t *testing.T) {
	page := `<p>kept</p><script>var a = "<p>no</p>";</script><style>p { color: red }</style>` +
		`<nav><p>menu</p><a href="/">home</a></nav><svg><title>chart</title><g>art</g></svg><header>head</header><footer>foot</footer><p>also kept</p>`
	got := htmlToText(page)
	if got != "kept\n\nalso kept\n" {
		t.Fatalf("got %q", got)
	}
}

func TestEntitiesAreDecodedAndCommentsAndDeclarationsAreDropped(t *testing.T) {
	page := `<!DOCTYPE html><!-- a <p>comment</p> --><?php hidden ?><p>a &amp; b &lt;c&gt; &#8364; &quot;d&quot;</p>`
	if got := htmlToText(page); got != "a & b <c> € \"d\"\n" {
		t.Fatalf("got %q", got)
	}
}

func TestAQuotedAttributeMayContainAGreaterThanSign(t *testing.T) {
	page := `<p title="a > b" data-x='<p>'>text</p>`
	if got := htmlToText(page); got != "text\n" {
		t.Fatalf("got %q", got)
	}
}

func TestASelfClosingBlockTagStartsAndEndsALine(t *testing.T) {
	// <br/> is a start and an end (two line breaks), <br> only a start (one)
	if got := htmlToText("a<br/>b"); got != "a\n\nb\n" {
		t.Errorf("<br/>: got %q", got)
	}
	if got := htmlToText("a<br>b"); got != "a\nb\n" {
		t.Errorf("<br>: got %q", got)
	}
}

func TestALessThanSignThatOpensNoTagIsText(t *testing.T) {
	if got := htmlToText("<p>1 < 2 and 3 <4</p>"); got != "1 < 2 and 3 <4\n" {
		t.Fatalf("got %q", got)
	}
	if got := htmlToText("a < "); got != "a <\n" {
		t.Fatalf("a lone '<' at the end: got %q", got)
	}
}

func TestTagNamesAreCaseInsensitiveAndAnUnterminatedMarkupEndsThePage(t *testing.T) {
	if got := htmlToText("<P>x</P><SCRIPT>y</SCRIPT>z"); got != "x\nz\n" {
		t.Errorf("upper case: got %q", got)
	}
	for _, page := range []string{"<p>x<!-- never closed", "<p>x</p><!doctype", "<p>x<p title=\"open"} {
		t.Run(page, func(t *testing.T) {
			if got := htmlToText(page); !strings.HasPrefix(got, "x\n") {
				t.Errorf("%q: got %q", page, got)
			}
		})
	}
	if got := htmlToText("<p>x</p><script>never closed"); got != "x\n" {
		t.Errorf("an unclosed script: got %q", got)
	}
}

func TestBlankRunsShrinkTrailingSpacesGoAndAllKindsOfLineBreakSplit(t *testing.T) {
	page := "<pre>a   \r\n\r\n\r\n\r\nb c\u0085d\x0be\x0cf\x1cg\n</pre>"
	// the end tag of the block adds one more line break after the last line, which stays as a blank line
	want := "a\n\nb\nc\nd\ne\nf\ng\n\n"
	if got := htmlToText(page); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := htmlToText(""); got != "\n" {
		t.Errorf("an empty page: got %q", got)
	}
	if got := htmlToText("<p>  </p>"); got != "\n" {
		t.Errorf("a page with only blanks: got %q", got)
	}
}

func TestAnEndTagWithoutItsStartDoesNotUnbalanceTheSkipping(t *testing.T) {
	if got := htmlToText("</nav><p>x</p>"); got != "x\n" {
		t.Fatalf("got %q", got)
	}
}
