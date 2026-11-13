package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// maxSites is how many places of use a row names before it says "...".
const maxSites = 3

// maxHelpers is how many changed helpers a "via" row names before it says "...".
const maxHelpers = 3

// report is what render prints.
type report struct {
	opts   options
	rng    revRange
	module string
	cmp    comparison
	use    *usage // nil without a consumer
	files  []fileCount
}

// render prints the report: the header, the exported symbols, the files touched by more than one commit.
func render(w io.Writer, r report) error {
	rows, hidden := r.visibleRows()
	pr := &lineWriter{w: w}
	pr.linef("seams %s..%s: %s, module %s", r.opts.from, r.opts.to, plural(r.rng.commits, "commit"), r.module)
	if r.use == nil {
		pr.linef("consumer: none (-provider DIR or %s): every changed exported symbol is listed", providerVariable)
	} else {
		pr.linef("consumer: Go files read: %d, importing the library: %d", r.use.files, r.use.library)
	}
	pr.linef("")
	pr.linef("Exported symbols: %d, %d changed themselves and %d reach a changed declaration%s",
		len(rows), len(rows)-via(rows), via(rows), hiddenNote(hidden))
	pr.table(r.symbolLines(rows))
	pr.linef("")
	pr.linef("Changed declarations that are not exported, with the exported symbols that reach them: %d", len(r.cmp.helpers))
	pr.table(r.helperLines())
	pr.linef("")
	pr.linef("Production files touched by more than one commit: %d", len(r.files))
	pr.table(r.fileLines())
	return pr.err
}

// visibleRows is the rows to print, and how many the consumer does not use. Without a consumer, or
// with -all, every row is printed.
func (r report) visibleRows() (rows []row, hidden int) {
	for _, rw := range r.cmp.rows {
		if r.use != nil && !r.opts.all && len(r.use.sitesOf(rw.node)) == 0 {
			hidden++
			continue
		}
		rows = append(rows, rw)
	}
	return rows, hidden
}

// plural is "1 commit", "2 commits".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// via counts the rows of symbols that did not change but reach a declaration that did.
func via(rows []row) int {
	n := 0
	for _, rw := range rows {
		if strings.HasPrefix(rw.what, viaPrefix) {
			n++
		}
	}
	return n
}

func hiddenNote(hidden int) string {
	if hidden == 0 {
		return ""
	}
	return fmt.Sprintf(" (and %d the consumer does not use; -all lists them)", hidden)
}

// symbolLines is the table of exported symbols.
func (r report) symbolLines(rows []row) [][]string {
	if len(rows) == 0 {
		return nil
	}
	header := []string{"SYMBOL", "CHANGE", "FILE"}
	if r.use != nil {
		header = append(header, "USED BY THE CONSUMER")
	}
	lines := [][]string{header}
	for _, rw := range rows {
		line := []string{rw.node.symbol(), r.limitHelpers(rw.what), rw.node.file}
		if r.use != nil {
			line = append(line, describeSites(r.use.sitesOf(rw.node), r.siteCap()))
		}
		lines = append(lines, line)
	}
	return lines
}

// helperLines is the table of changed declarations no caller can name.
func (r report) helperLines() [][]string {
	if len(r.cmp.helpers) == 0 {
		return nil
	}
	header := []string{"DECLARATION", "CHANGE", "FILE", "REACHED BY"}
	lines := [][]string{header}
	for _, h := range r.cmp.helpers {
		reached := plural(len(h.users), "exported symbol")
		if r.use != nil {
			reached += fmt.Sprintf(", %d used by the consumer", r.usedBy(h.users))
		}
		lines = append(lines, []string{h.node.symbol(), h.what, h.node.file, reached})
	}
	return lines
}

// usedBy counts the declarations of the list the consumer uses.
func (r report) usedBy(list []*node) int {
	n := 0
	for _, d := range list {
		if len(r.use.sitesOf(d)) > 0 {
			n++
		}
	}
	return n
}

// limitHelpers shortens the list of helpers of a "via" row, unless every one was asked for.
func (r report) limitHelpers(what string) string {
	list, ok := strings.CutPrefix(what, viaPrefix)
	if !ok || r.opts.all {
		return what
	}
	names := strings.Split(list, ", ")
	if len(names) <= maxHelpers {
		return what
	}
	return viaPrefix + strings.Join(names[:maxHelpers], ", ") + fmt.Sprintf(", ... (%d more)", len(names)-maxHelpers)
}

// siteCap is the limit of places per row: none with -all.
func (r report) siteCap() int {
	if r.opts.all {
		return 0
	}
	return maxSites
}

// fileLines is the table of files touched more than once.
func (r report) fileLines() [][]string {
	if len(r.files) == 0 {
		return nil
	}
	lines := [][]string{{"COMMITS", "FILE"}}
	for _, f := range r.files {
		lines = append(lines, []string{strconv.Itoa(f.commits), f.file})
	}
	return lines
}

// lineWriter writes lines and keeps the first error, so the callers need not check each write.
type lineWriter struct {
	w   io.Writer
	err error
}

func (p *lineWriter) linef(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format+"\n", args...)
	}
}

// table writes rows as aligned columns, indented by two spaces.
func (p *lineWriter) table(rows [][]string) {
	if p.err != nil {
		return
	}
	tw := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, "  "+strings.Join(row, "\t"))
	}
	if err := tw.Flush(); err != nil {
		p.err = err
	}
}
