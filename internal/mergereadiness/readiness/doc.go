// Package readiness judges the "Merge readiness" section of a pull request description: it lists the
// boxes of the section that are not ticked and says whether the pull request may pass the check. It is
// the library part of the command internal/mergereadiness, a build tool of this repository.
//
// The description is untrusted input and is read as text: no regular expression is applied to it,
// every scan is a loop over the characters, so the time is linear in its size whatever it contains.
//
// # What counts
//
// The section starts at the heading "## Merge readiness" (the title is compared without regard to case
// and with its whitespace collapsed; up to three spaces may precede the heading) and ends at the next
// heading of level 1 or 2, so a "### Pending work" subsection belongs to it. Only ATX headings
// ("## Title") are read, a Setext heading (a line underlined with --- or ===) is not. A box is a list
// item, with the marker -, * or +, whose text starts with [ ] (unticked) or [x] or [X] (ticked).
// Lines end with LF, CRLF or CR.
//
// # Stricter than the renderer, and cross-checked
//
// A second reading of the same text decides the cases the first one gets wrong, because the renderer
// of the hosting service is the only judge of what a reader sees and this package is not it. The
// structured reading follows headings, fenced code blocks and HTML comments: a fence is opened by a
// line that starts with it, a comment hides text only when it starts a line and is closed later, and
// neither combines with the other on one line. The naive reading knows no structure: after a heading
// titled "Merge readiness" at any level (a quoted heading too) it counts every line that has the
// shape of an unticked task list item, with any list marker, nested markers, a quote prefix, a
// numbered item, a tab or a no-break space, in code and comments as well, up to the next heading of
// level 1 or 2. When the naive reading finds more unticked boxes than the structured one, the markup
// is ambiguous and the verdict is red: a box that this package does not understand must not turn the
// check green. A section with no box at all is red too, because it is the template's section with its
// markup damaged. The same stricter side is taken where the renderer is the more lenient one: a box in
// an indented code block, in an HTML block, in a fence or a comment inside the section, an empty
// "- [ ]" item, or a heading that quotes the section inside a fence, is reported; the output names the
// line, and the author reshapes the text or ticks the box. A heading of the section in a block that
// the renderer does not read as a heading is reported the same way.
//
// A description without the heading, an empty one and an unset variable pass with a message. A
// description with the section twice is judged section by section and is red when any box of any of
// them is unticked.
//
// # Output
//
// A verdict is a few lines. A box is printed after the fixed prefix "  - [ ] ", so a line of the
// output never starts with the text of a box; the runner of the hosting service trims leading blanks
// before it looks for a workflow command, so the fixed prefix is what protects, together with the
// sanitizing: every control character in a box's text is replaced with "?" and a line is cut at 200
// characters. At most 50 boxes are listed, then a line "... and N more".
package readiness
