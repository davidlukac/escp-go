"""Tests of scripts/merge-readiness.py: run by `make mergereadiness` (part of `make lint`)."""

import importlib.util
import io
import os
import subprocess
import sys
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "merge-readiness.py")
TEMPLATE = os.path.join(HERE, "..", ".github", "pull_request_template.md")

spec = importlib.util.spec_from_file_location("merge_readiness", SCRIPT)
mr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mr)

# The block of the pull request template, with every box unticked, and the same with every box ticked.
SECTION = """## Merge readiness

<!-- Kept by the maintainer's assistant. -->

- [ ] CI is green on the head commit
- [ ] Local checks done
- [ ] **Ready for the owner to merge**

### Pending work

- [ ] a task (reviewer)
"""
TICKED = SECTION.replace("[ ]", "[x]")


def run(body):
    """The status and the output of the script for a body."""
    out = io.StringIO()
    status = mr.main({mr.BODY_VARIABLE: body}, out)
    return status, out.getvalue()


class VerdictTest(unittest.TestCase):
    def test_cases(self):
        cases = [
            ("every box unticked", "Why.\n\n" + SECTION, 1, "4 box(es) not ticked"),
            ("every box ticked", "Why.\n\n" + TICKED, 0, mr.MSG_ALL_TICKED),
            ("one box left", TICKED.replace("[x] Local", "[ ] Local"), 1, "1 box(es) not ticked"),
            ("capital X is ticked", SECTION.replace("[ ]", "[X]"), 0, mr.MSG_ALL_TICKED),
            ("empty body", "", 0, mr.MSG_NO_SECTION),
            ("only blank lines", "\n\n  \n", 0, mr.MSG_NO_SECTION),
            ("no section", "## What and why\n\n- [ ] a box of another section\n", 0, mr.MSG_NO_SECTION),
            ("section without boxes", "## Merge readiness\n\nnothing here\n", 0, mr.MSG_NO_BOXES),
            ("the heading is not level 2", "### Merge readiness\n\n- [ ] x\n", 0, mr.MSG_NO_SECTION),
            ("a level 1 heading is not the section", "# Merge readiness\n\n- [ ] x\n", 0, mr.MSG_NO_SECTION),
            ("the title is matched whole", "## Merge readiness notes\n\n- [ ] x\n", 0, mr.MSG_NO_SECTION),
            ("the title ignores case", "## MERGE READINESS\n\n- [ ] x\n", 1, "1 box(es) not ticked"),
            ("a closing sequence on the heading", "## Merge readiness ##\n\n- [ ] x\n", 1, "1 box(es)"),
            ("three spaces before the heading", "   ## Merge readiness\n- [ ] x\n", 1, "1 box(es)"),
            ("four spaces make code, not a heading", "    ## Merge readiness\n- [ ] x\n", 0, mr.MSG_NO_SECTION),
        ]
        for name, body, status, text in cases:
            with self.subTest(name):
                got_status, got = run(body)
                self.assertEqual(got_status, status, got)
                self.assertIn(text, got)

    def test_other_sections_do_not_count(self):
        body = "## What\n\n- [ ] before\n\n" + TICKED + "\n## Checklist\n\n- [ ] after\n"
        self.assertEqual(run(body), (0, mr.MSG_ALL_TICKED + "\n"))
        # A level 1 heading ends the section too.
        body = TICKED + "\n# Notes\n\n- [ ] after\n"
        self.assertEqual(run(body)[0], 0)

    def test_the_pending_work_subsection_belongs_to_the_section(self):
        body = "## Merge readiness\n\n- [x] a\n\n### Pending work\n\n- [ ] queued (implementer)\n"
        status, out = run(body)
        self.assertEqual(status, 1)
        self.assertIn("  - [ ] queued (implementer)", out)

    def test_list_markers_and_indentation(self):
        for marker in ["-", "*", "+"]:
            for indent in ["", "  ", "\t"]:
                with self.subTest(marker=marker, indent=indent):
                    body = "## Merge readiness\n%s%s [ ] a box\n" % (indent, marker)
                    self.assertEqual(run(body)[0], 1)

    def test_text_that_is_not_a_box(self):
        lines = [
            "[ ] no list marker",
            "- [] empty brackets",
            "- [  ] two spaces",
            "- [ ]no space after the box is not a box item",
            "- [y] another letter",
            "- plain item [ ] with the box inside",
        ]
        for line in lines:
            with self.subTest(line):
                self.assertEqual(run("## Merge readiness\n\n" + line + "\n- [x] done\n")[0], 0)

    def test_a_box_without_text_is_still_a_box(self):
        status, out = run("## Merge readiness\n- [ ]\n")
        self.assertEqual((status, "  - [ ] \n" in out), (1, True))

    def test_line_endings(self):
        for name, newline in [("CRLF", "\r\n"), ("CR", "\r"), ("LF", "\n")]:
            with self.subTest(name):
                status, out = run(SECTION.replace("\n", newline))
                self.assertEqual(status, 1)
                self.assertIn("4 box(es)", out)
                self.assertNotIn("\r", out)
                self.assertEqual(run(TICKED.replace("\n", newline))[0], 0)

    def test_two_sections_fail_closed(self):
        cases = [
            ("first unticked", SECTION + "\n## Notes\n\ntext\n\n" + TICKED, 4),
            ("second unticked", TICKED + "\n## Notes\n\ntext\n\n" + SECTION, 4),
            ("both ticked", TICKED + "\n## Notes\n\n" + TICKED, 0),
        ]
        for name, body, count in cases:
            with self.subTest(name):
                status, out = run(body)
                self.assertEqual(status, 1 if count else 0)
                if count:
                    self.assertIn("%d box(es)" % count, out)

    def test_two_sections_are_judged_section_by_section(self):
        self.assertEqual(mr.unticked_boxes(SECTION + TICKED)[0], 2)


class MarkdownStructureTest(unittest.TestCase):
    def test_a_heading_in_a_fence_starts_no_section(self):
        for fence in ["```", "~~~", "````"]:
            with self.subTest(fence):
                body = "Example:\n\n%s\n%s%s\n" % (fence, SECTION, fence)
                self.assertEqual(run(body), (0, mr.MSG_NO_SECTION + "\n"))

    def test_a_heading_in_a_fence_does_not_end_the_section(self):
        body = "## Merge readiness\n\n```\n## Other\n```\n\n- [ ] after the fence\n"
        self.assertEqual(run(body)[0], 1)

    def test_boxes_in_a_fence_inside_the_section_do_not_count(self):
        body = "## Merge readiness\n\n- [x] done\n\n```\n- [ ] quoted\n```\n"
        self.assertEqual(run(body), (0, mr.MSG_ALL_TICKED + "\n"))

    def test_fence_closing_rules(self):
        cases = [
            ("a shorter closing fence does not close", "````\n```\n- [ ] x\n````\n", 0),
            ("another character does not close", "```\n~~~\n- [ ] x\n```\n", 0),
            ("text after the closing fence does not close", "```\n``` text\n- [ ] x\n```\n", 0),
            ("a longer closing fence closes", "```\n- [ ] x\n`````\n- [ ] y\n", 1),
            ("an unclosed fence runs to the end", "```\n- [ ] x\n", 0),
            ("a backtick in the info string of a backtick fence is no fence", "``` a`b\n- [ ] x\n", 1),
            ("a tilde fence may have a backtick in its info", "~~~ a`b\n- [ ] x\n~~~\n", 0),
        ]
        for name, rest, status in cases:
            with self.subTest(name):
                self.assertEqual(run("## Merge readiness\n\n" + rest)[0], status)

    def test_a_box_in_a_comment_does_not_count(self):
        cases = [
            ("one line", "<!-- - [ ] hidden -->\n- [x] a\n"),
            ("several lines", "<!--\n- [ ] hidden\nmore\n-->\n- [x] a\n"),
            ("after a box on the same line", "- [x] a <!-- - [ ] b -->\n"),
            ("two comments on a line", "<!-- a --> <!-- - [ ] b -->\n- [x] a\n"),
            ("the template comment", SECTION.split("- [ ]")[0] + "- [x] a\n"),
        ]
        for name, rest in cases:
            with self.subTest(name):
                self.assertEqual(run("## Merge readiness\n\n" + rest)[0], 0)

    def test_a_comment_closes_and_what_follows_it_counts(self):
        body = "## Merge readiness\n\n<!-- a\nb --> - [ ] visible\n"
        # Markdown may render this line as part of the comment's HTML block; the check fails closed.
        self.assertEqual(run(body)[0], 1)
        body = "## Merge readiness\n\n<!-- a\nb -->\n- [ ] visible\n"
        self.assertEqual(run(body)[0], 1)

    def test_a_heading_in_a_comment_neither_starts_nor_ends_a_section(self):
        body = "<!--\n## Merge readiness\n-->\n- [ ] x\n"
        self.assertEqual(run(body)[0], 0)
        body = "## Merge readiness\n\n<!--\n## Other\n-->\n- [ ] x\n"
        self.assertEqual(run(body)[0], 1)

    def test_a_fence_in_a_comment_is_no_fence(self):
        body = "## Merge readiness\n\n<!--\n```\n-->\n- [ ] x\n"
        self.assertEqual(run(body)[0], 1)

    def test_a_comment_in_a_fence_is_text(self):
        body = "## Merge readiness\n\n```\n<!--\n```\n\n- [ ] x\n"
        self.assertEqual(run(body)[0], 1)

    def test_an_unclosed_comment_hides_the_rest(self):
        # The way the body renders: the text is not shown, so it is not judged.
        self.assertEqual(run("## Merge readiness\n- [x] a\n<!-- a\n- [ ] x\n")[0], 0)

    def test_the_template_of_the_repository(self):
        with open(TEMPLATE, encoding="utf-8") as handle:
            template = handle.read()
        status, out = run(template)
        self.assertEqual(status, 1, out)
        self.assertIn("  - [ ] <task> (<agent role>)", out)
        # The author's checklist above the section is not judged: only the section's own boxes.
        self.assertNotIn("Title follows Conventional Commits", out)
        self.assertEqual(run(template.split("## Merge readiness")[0])[1], mr.MSG_NO_SECTION + "\n")
        ticked = template[: template.index("## Merge readiness")] + TICKED
        self.assertEqual(run(ticked), (0, mr.MSG_ALL_TICKED + "\n"))


class OutputTest(unittest.TestCase):
    def test_the_unticked_boxes_are_listed_in_order(self):
        status, out = run(SECTION)
        self.assertEqual(status, 1)
        self.assertEqual(
            out.splitlines(),
            [
                "merge readiness: 4 box(es) not ticked:",
                "  - [ ] CI is green on the head commit",
                "  - [ ] Local checks done",
                "  - [ ] **Ready for the owner to merge**",
                "  - [ ] a task (reviewer)",
            ],
        )

    def test_no_line_starts_with_a_workflow_command(self):
        status, out = run("## Merge readiness\n- [ ] ::error::boom\n- [ ] \x1b[31mred\n")
        self.assertEqual(status, 1)
        for line in out.splitlines():
            self.assertFalse(line.startswith("::"), line)
        self.assertNotIn("\x1b", out)

    def test_a_long_line_is_cut(self):
        _, out = run("## Merge readiness\n- [ ] " + "a" * 5000 + "\n")
        longest = max(len(line) for line in out.splitlines())
        self.assertLessEqual(longest, len("  - [ ] ") + mr.MAX_LINE)
        self.assertIn("...", out)

    def test_many_boxes_are_counted_and_the_list_is_cut(self):
        count = mr.MAX_LISTED + 7
        status, out = run("## Merge readiness\n" + "- [ ] box\n" * count)
        self.assertEqual(status, 1)
        self.assertIn("%d box(es) not ticked" % count, out)
        self.assertEqual(out.count("  - [ ] box"), mr.MAX_LISTED)
        self.assertIn("... and 7 more", out)

    def test_exactly_the_most_listed_has_no_remainder(self):
        _, out = run("## Merge readiness\n" + "- [ ] box\n" * mr.MAX_LISTED)
        self.assertNotIn("more", out)


class LargeBodyTest(unittest.TestCase):
    def test_a_very_long_body_is_read_in_moments(self):
        filler = "text with [ ] and <!-- a comment --> and ``` and ## inside a line\n" * 20000
        body = filler + SECTION + filler
        started = time.monotonic()
        status, out = run(body)
        elapsed = time.monotonic() - started
        self.assertEqual(status, 1)
        self.assertIn("4 box(es)", out)
        # A generous bound for a linear scan of about a megabyte; a quadratic one would miss it by far.
        self.assertLess(elapsed, 10)

    def test_a_long_line_of_backticks_and_spaces_is_read_in_moments(self):
        started = time.monotonic()
        run("## Merge readiness\n" + "-" + " " * 200000 + "x\n" + "#" * 100000 + " \n" + " " * 200000 + "\n")
        self.assertLess(time.monotonic() - started, 10)


class ProgramTest(unittest.TestCase):
    """The script as the workflow runs it: a process with the body in its environment."""

    def run_script(self, env):
        full = {key: value for key, value in os.environ.items() if key != mr.BODY_VARIABLE}
        full.update(env)
        return subprocess.run(
            [sys.executable, SCRIPT], env=full, capture_output=True, text=True, check=False, timeout=60
        )

    def test_unticked_exits_one(self):
        result = self.run_script({mr.BODY_VARIABLE: SECTION})
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("  - [ ] CI is green on the head commit", result.stdout)

    def test_ticked_exits_zero(self):
        result = self.run_script({mr.BODY_VARIABLE: TICKED})
        self.assertEqual((result.returncode, result.stdout), (0, mr.MSG_ALL_TICKED + "\n"))

    def test_an_unset_variable_passes(self):
        result = self.run_script({})
        self.assertEqual((result.returncode, result.stdout), (0, mr.MSG_NO_SECTION + "\n"))

    def test_an_empty_variable_passes(self):
        result = self.run_script({mr.BODY_VARIABLE: ""})
        self.assertEqual((result.returncode, result.stdout), (0, mr.MSG_NO_SECTION + "\n"))

    def test_the_body_is_not_taken_from_the_arguments(self):
        result = subprocess.run(
            [sys.executable, SCRIPT, SECTION],
            env={"PATH": os.environ.get("PATH", "")},
            capture_output=True,
            text=True,
            check=False,
            timeout=60,
        )
        self.assertEqual((result.returncode, result.stdout), (0, mr.MSG_NO_SECTION + "\n"))

    def test_the_default_is_the_process_environment(self):
        out = io.StringIO()
        old = os.environ.get(mr.BODY_VARIABLE)
        os.environ[mr.BODY_VARIABLE] = SECTION
        try:
            self.assertEqual(mr.main(out=out), 1)
        finally:
            if old is None:
                del os.environ[mr.BODY_VARIABLE]
            else:
                os.environ[mr.BODY_VARIABLE] = old
        self.assertIn("4 box(es)", out.getvalue())


if __name__ == "__main__":
    unittest.main()
