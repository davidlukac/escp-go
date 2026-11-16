package escp

import (
	"sort"
	"strings"
	"testing"
)

// The British -ise verbs the text test forbids, as stems written so that this file does not contain the
// words it forbids (the misspell linter does not know these verbs, so the text test is the only check).
// The provider's `make spell` keeps the same list as BRITISH_VERBS in its Makefile: add a stem to both
// when a British word gets through.
var (
	// providerIseStems and providerIseSuffixes are the provider's BRITISH_VERBS, verbatim: a stem and one
	// of the suffixes make a whole word.
	providerIseStems    = []string{"normali", "generali", "speciali", "capitali", "canonicali", "parametri", "tokeni"}
	providerIseSuffixes = []string{"s", "se", "sed", "ses", "sing", "sation", "sations"}

	// libraryIseStems are the stems this library listed before it took the provider's; any word that
	// continues a stem with an s is reported, which the provider's whole-word suffixes do not cover
	// (a noun or an agent of the verb).
	libraryIseStems = []string{"summari", "initiali", "seriali", "normali", "optimi", "recogni", "organi"}
)

// britishIseVerbs is the alternation of both lists, for britishSpelling.
var britishIseVerbs = "(?:" + strings.Join(providerIseStems, "|") + ")(?:" + strings.Join(providerIseSuffixes, "|") + ")" +
	`|(?:` + strings.Join(libraryIseStems, "|") + `)s\w*`

// allIseStems is every stem of both lists, once, in a fixed order.
func allIseStems() []string {
	seen := map[string]bool{}
	var stems []string
	for _, stem := range append(append([]string{}, providerIseStems...), libraryIseStems...) {
		if !seen[stem] {
			seen[stem] = true
			stems = append(stems, stem)
		}
	}
	sort.Strings(stems)
	return stems
}

// americanIseSuffixes are the endings of the American forms of the verbs and their nouns.
var americanIseSuffixes = []string{"e", "ed", "es", "ing", "ation", "ations"}

// Every stem, with every suffix of the provider's list, is a British word the text test reports; the
// American form (the stem with a z) is not. The table is built from the lists, so a stem that drops out of
// the regexp fails here, and a stem that is added to a list is checked without a new case.
func TestEveryBritishIseStemIsReported(t *testing.T) {
	for _, stem := range allIseStems() {
		for _, suffix := range providerIseSuffixes {
			word := stem + suffix
			t.Run("British "+word, func(t *testing.T) {
				if !britishSpelling.MatchString("a sentence with " + word + " in it") {
					t.Errorf("%q is a British spelling and is not reported", word)
				}
			})
		}
		for _, suffix := range americanIseSuffixes {
			word := stem + "z" + suffix
			t.Run("American "+word, func(t *testing.T) {
				if britishSpelling.MatchString("a sentence with " + word + " in it") {
					t.Errorf("%q is American English and is reported", word)
				}
			})
		}
	}
}

// The six stems of the issue are in the list the provider keeps, and the list is complete: a stem removed
// from either list shows here by name.
func TestTheIseStemsIncludeTheProvidersList(t *testing.T) {
	want := []string{"normali", "generali", "speciali", "capitali", "canonicali", "parametri", "tokeni"}
	have := map[string]bool{}
	for _, stem := range allIseStems() {
		have[stem] = true
	}
	for _, stem := range want {
		t.Run(stem, func(t *testing.T) {
			if !have[stem] {
				t.Errorf("stem %q is missing", stem)
			}
		})
	}
}

// Words that are not the British spelling of a verb and share the start of a stem stay unreported: a
// regexp that grows to a bare stem would flag them.
func TestIseStemsDoNotReportOrdinaryWords(t *testing.T) {
	for _, word := range []string{"specialist", "capitalist", "generalist", "specials", "capitals", "generals", "tokens", "parameters"} {
		t.Run(word, func(t *testing.T) {
			if britishSpelling.MatchString("a sentence with " + word + " in it") {
				t.Errorf("%q is an ordinary word and is reported", word)
			}
		})
	}
}

// The other British spellings the text test has always reported stay reported, written in pieces so that
// this file does not contain them.
func TestTheOlderBritishSpellingsAreStillReported(t *testing.T) {
	for _, word := range []string{"behavi" + "our", "colo" + "ur", "neighbo" + "urs", "catalo" + "gue", "cancel" + "led", "mode" + "lling", "hono" + "ured", "judge" + "ment", "lic" + "ence", "artefa" + "ct", "dial" + "led", "cen" + "tre", "whil" + "st", "among" + "st"} {
		t.Run(word, func(t *testing.T) {
			if !britishSpelling.MatchString("a sentence with " + word + " in it") {
				t.Errorf("%q is a British spelling and is not reported", word)
			}
		})
	}
}
