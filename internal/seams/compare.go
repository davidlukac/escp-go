package main

import (
	"slices"
	"sort"
	"strings"
)

// What changed about a symbol, as the report words it.
const (
	changeAdded     = "added"
	changeRemoved   = "removed"
	changeSignature = "signature" // a function or a method
	changeValue     = "value"     // a constant or a variable
	changeType      = "type"      // a type
	changeBody      = "body"
	// viaPrefix starts the word of a symbol that is itself unchanged but calls, or reads, one that is.
	viaPrefix = "via "
)

// row is one exported symbol the report lists.
type row struct {
	node *node  // the declaration in the newer revision, or in the older one for a removal
	what string // what changed
}

// comparison is what differs between two revisions.
type comparison struct {
	rows    []row           // exported symbols, sorted by name
	helpers []helper        // changed declarations that are not exported, sorted by file and name
	changed map[string]bool // keys of the declarations (exported or not) present in both revisions that differ
}

// helper is a changed declaration no caller can name, and the exported symbols that reach it.
type helper struct {
	node  *node
	what  string
	users []*node // the exported declarations whose calls lead to it, sorted by name
}

// compare lists the exported symbols that were added, removed or changed, and the exported symbols that
// reach a changed declaration without changing themselves.
func compare(from, to *tree) comparison {
	c := comparison{changed: map[string]bool{}}
	var direct []row
	for key, n := range to.nodes {
		old, ok := from.nodes[key]
		switch {
		case !ok && n.exported:
			direct = append(direct, row{n, changeAdded})
		case !ok:
			continue
		case describe(old, n) != "":
			c.changed[key] = true
			if n.exported {
				direct = append(direct, row{n, describe(old, n)})
			}
		}
	}
	for key, old := range from.nodes {
		if _, ok := to.nodes[key]; !ok && old.exported {
			direct = append(direct, row{old, changeRemoved})
		}
	}
	idx := newIndex(to)
	direct = append(direct, reaching(idx, from, c.changed)...)
	c.rows = direct
	sort.Slice(c.rows, func(i, j int) bool {
		return c.rows[i].node.symbol() < c.rows[j].node.symbol()
	})
	c.helpers = helpers(idx, from, c.changed)
	return c
}

// describe says what differs between two declarations of the same name: "signature" (the type of a
// function), "value" (of a constant or a variable), "type" (the definition of a type), "body", or two
// of them; "" when they are the same code.
func describe(old, n *node) string {
	var parts []string
	if old.sig != n.sig {
		parts = append(parts, sigWord(n))
	}
	if old.body != n.body {
		parts = append(parts, changeBody)
	}
	return strings.Join(parts, ", ")
}

// sigWord names what the signature of a declaration of this kind is.
func sigWord(n *node) string {
	switch n.kind {
	case kindConst, kindVar:
		return changeValue
	case kindType:
		return changeType
	default:
		return changeSignature
	}
}

// reaching finds the exported symbols that did not change but call, or read, a declaration that did: the
// nearest changed declarations on each path are named. The graph is by name, without types, so it
// over-reports: a method named like another type's method is taken for the same one. A declaration
// added in this range is not one that changed.
func reaching(idx *index, from *tree, changed map[string]bool) []row {
	var out []row
	for key, n := range idx.t.nodes {
		if _, existed := from.nodes[key]; !existed || !n.exported || changed[key] {
			continue
		}
		if hit := idx.nearest(n, changed); len(hit) > 0 {
			out = append(out, row{n, viaPrefix + strings.Join(hit, ", ")})
		}
	}
	return out
}

// helpers finds, for each changed declaration that is not exported, the exported symbols that reach it
// by any path: a fix in a helper changes everything above it, not only what calls it directly.
func helpers(idx *index, from *tree, changed map[string]bool) []helper {
	found := map[string]*helper{}
	for _, n := range idx.t.nodes {
		if !n.exported {
			continue
		}
		for _, v := range idx.reach(n) {
			if !changed[v.key()] || v.exported {
				continue
			}
			if found[v.key()] == nil {
				found[v.key()] = &helper{node: v, what: describe(from.nodes[v.key()], v)}
			}
			found[v.key()].users = append(found[v.key()].users, n)
		}
	}
	out := make([]helper, 0, len(found))
	for _, h := range found {
		sort.Slice(h.users, func(i, j int) bool { return h.users[i].symbol() < h.users[j].symbol() })
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].node.file != out[j].node.file {
			return out[i].node.file < out[j].node.file
		}
		return out[i].node.symbol() < out[j].node.symbol()
	})
	return out
}

// index resolves the names a declaration mentions to declarations of its package.
type index struct {
	t       *tree
	funcs   map[string]*node   // package-level declarations by directory and name
	methods map[string][]*node // methods by directory and name
}

func newIndex(t *tree) *index {
	idx := &index{t: t, funcs: map[string]*node{}, methods: map[string][]*node{}}
	for _, n := range t.nodes {
		if n.kind == kindType {
			continue // a type is not called: its change is listed for the type itself
		}
		if n.recv == "" {
			idx.funcs[n.dir+"|"+n.name] = n
		} else {
			idx.methods[n.dir+"|"+n.name] = append(idx.methods[n.dir+"|"+n.name], n)
		}
	}
	return idx
}

// next lists the declarations n mentions.
func (idx *index) next(n *node) []*node {
	var out []*node
	for _, id := range n.calls.idents {
		if m, ok := idx.funcs[n.dir+"|"+id]; ok {
			out = append(out, m)
		}
	}
	for _, sel := range n.calls.selectors {
		out = append(out, idx.resolveMethod(n, sel)...)
	}
	return out
}

// resolveMethod finds the methods a call of a method named name, made in n, can mean. A name only one
// type of the package declares means that one. A name several types declare (String, Validate) means
// those of them that n mentions: its own receiver and the types it names; without types that is the
// best guess, and a call through a field whose type n does not name is missed.
func (idx *index) resolveMethod(n *node, name string) []*node {
	all := idx.methods[n.dir+"|"+name]
	if len(all) <= 1 {
		return all
	}
	var out []*node
	for _, m := range all {
		if m.recv == n.recv || slices.Contains(n.calls.idents, m.recv) {
			out = append(out, m)
		}
	}
	return out
}

// walk returns every declaration n leads to through calls; it goes on past a declaration only when
// expand says so.
func (idx *index) walk(n *node, expand func(*node) bool) []*node {
	seen := map[string]bool{n.key(): true}
	queue := []*node{n}
	var out []*node
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, m := range idx.next(cur) {
			if seen[m.key()] {
				continue
			}
			seen[m.key()] = true
			out = append(out, m)
			if expand(m) {
				queue = append(queue, m)
			}
		}
	}
	return out
}

// reach returns every declaration n leads to, through any number of calls.
func (idx *index) reach(n *node) []*node {
	return idx.walk(n, func(*node) bool { return true })
}

// nearest returns, sorted, the changed declarations n meets first on each path (the walk does not go on
// beyond a changed declaration).
func (idx *index) nearest(n *node, changed map[string]bool) []string {
	var hit []string
	for _, m := range idx.walk(n, func(m *node) bool { return !changed[m.key()] }) {
		if changed[m.key()] {
			hit = append(hit, helperName(m))
		}
	}
	sort.Strings(hit)
	return slices.Compact(hit)
}

// helperName is the name of a changed declaration inside its package: Client.drain, settle.
func helperName(n *node) string {
	if n.recv == "" {
		return n.name
	}
	return n.recv + "." + n.name
}
