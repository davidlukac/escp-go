#!/usr/bin/env python3
"""Fail while a box of the "Merge readiness" section of a pull request body is unticked.

The pull request body is read from the environment variable PR_BODY (never from the command line or
from text a workflow interpolated into a script: the body is untrusted input). The section starts at
the heading "## Merge readiness" and ends at the next heading of level 1 or 2, so a "### Pending
work" subsection belongs to it. What counts:

  * an unticked box is a list item (-, * or +, indented or not) that starts with "[ ]";
  * "[x]" and "[X]" are ticked;
  * headings, fenced code blocks (``` or ~~~) and HTML comments are read as Markdown renders them:
    a heading or a box inside a fence or a comment is text, not structure;
  * only ATX headings ("## Title") are read; a Setext heading (a line underlined with --- or ===) is
    not, so a section can only run longer, never shorter, than it renders;
  * a body without the section, an empty body and an unset variable pass with a message;
  * a body with the section twice is judged section by section, and fails when any box of any
    of them is unticked (fail closed: a second, ticked copy cannot hide an unticked first one).

Exit status: 0 when nothing is unticked (or there is no section), 1 when a box is unticked.
Standard library only; the tests are scripts/merge_readiness_test.py.
"""

import os
import re
import sys

BODY_VARIABLE = "PR_BODY"

SECTION_TITLE = "merge readiness"
SECTION_LEVEL = 2

# The most boxes listed, and the longest line printed, so that a hostile body cannot flood the log.
MAX_LISTED = 50
MAX_LINE = 200

MSG_NO_SECTION = "no merge readiness section: nothing to check"
MSG_NO_BOXES = "the merge readiness section has no boxes: nothing to check"
MSG_ALL_TICKED = "every box of the merge readiness section is ticked"

# An ATX heading: up to three spaces, one to six #, then a space or the end of the line.
HEADING = re.compile(r"^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$")
# The optional closing sequence of an ATX heading ("## Title ##").
HEADING_CLOSE = re.compile(r"[ \t]+#+$")
# A fence: up to three spaces, three or more backticks or tildes, then the info string.
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})(.*)$")
# A list item that starts with an unticked box.
UNTICKED = re.compile(r"^[ \t]*[-*+][ \t]+\[ \](?:[ \t]+(.*))?$")
# A list item that starts with a ticked box.
TICKED = re.compile(r"^[ \t]*[-*+][ \t]+\[[xX]\](?:[ \t]+.*)?$")

COMMENT_OPEN = "<!--"
COMMENT_CLOSE = "-->"


def strip_comments(line, in_comment):
    """Return the line without its HTML comments and whether a comment is still open after it."""
    kept = []
    pos = 0
    while pos < len(line):
        if in_comment:
            end = line.find(COMMENT_CLOSE, pos)
            if end < 0:
                return "".join(kept), True
            pos = end + len(COMMENT_CLOSE)
            in_comment = False
            continue
        start = line.find(COMMENT_OPEN, pos)
        if start < 0:
            kept.append(line[pos:])
            break
        kept.append(line[pos:start])
        pos = start + len(COMMENT_OPEN)
        in_comment = True
    return "".join(kept), in_comment


def heading_of(line):
    """Return (level, title) when the line is an ATX heading, else None."""
    found = HEADING.match(line)
    if not found:
        return None
    title = HEADING_CLOSE.sub("", found.group(2) or "").strip()
    return len(found.group(1)), title


def closes_fence(line, fence):
    """Whether the line closes the open fence: the same character, at least as long, nothing after."""
    found = FENCE.match(line)
    if not found or found.group(2).strip():
        return False
    marker = found.group(1)
    return marker[0] == fence[0] and len(marker) >= len(fence)


def visible_lines(body):
    """Yield the lines of the body that are Markdown structure or text: not code, not comments.

    A line inside a fenced code block is yielded as None, so that the caller still sees the line
    count but never reads it.
    """
    in_comment = False
    fence = ""
    for raw in body.replace("\r\n", "\n").replace("\r", "\n").split("\n"):
        if fence:
            if closes_fence(raw, fence):
                fence = ""
            yield None
            continue
        line, in_comment = strip_comments(raw, in_comment)
        opening = FENCE.match(line)
        # A fence opened in a comment is not one; a backtick fence's info string has no backtick.
        if opening and not (opening.group(1)[0] == "`" and "`" in opening.group(2)):
            fence = opening.group(1)
            yield None
            continue
        yield line


def unticked_boxes(body):
    """Return (sections, unticked, ticked): how many merge readiness sections the body has, the text
    of every unticked box in them and the number of ticked ones."""
    sections = 0
    unticked = []
    ticked = 0
    inside = False
    for line in visible_lines(body):
        if line is None:
            continue
        head = heading_of(line)
        if head is not None and head[0] <= SECTION_LEVEL:
            inside = head[0] == SECTION_LEVEL and head[1].lower() == SECTION_TITLE
            sections += inside
            continue
        if not inside:
            continue
        found = UNTICKED.match(line)
        if found:
            unticked.append((found.group(1) or "").strip())
        elif TICKED.match(line):
            ticked += 1
    return sections, unticked, ticked


def printable(text):
    """The text with control characters replaced and cut to a length a log line can carry."""
    clean = "".join(ch if ch.isprintable() else "?" for ch in text)
    return clean if len(clean) <= MAX_LINE else clean[: MAX_LINE - 3] + "..."


def report(body, out):
    """Write the verdict for the body to out and return the exit status."""
    sections, unticked, ticked = unticked_boxes(body)
    if sections == 0:
        print(MSG_NO_SECTION, file=out)
        return 0
    if not unticked:
        print(MSG_ALL_TICKED if ticked else MSG_NO_BOXES, file=out)
        return 0
    print("merge readiness: %d box(es) not ticked:" % len(unticked), file=out)
    # Every line starts with spaces: a line that starts with "::" would be read as a workflow command.
    for text in unticked[:MAX_LISTED]:
        print("  - [ ] " + printable(text), file=out)
    if len(unticked) > MAX_LISTED:
        print("  ... and %d more" % (len(unticked) - MAX_LISTED), file=out)
    return 1


def main(environ=None, out=None):
    environ = os.environ if environ is None else environ
    out = sys.stdout if out is None else out
    return report(environ.get(BODY_VARIABLE, ""), out)


if __name__ == "__main__":
    sys.exit(main())
