// Package markdown parses a SKILL.md body into the small structured view the
// rubric needs, using goldmark (the established Go Markdown parser) instead of
// hand-rolled regexes. Doing the parsing properly means code fences, headings,
// lists, tables, links, and code spans are AST facts — so, for example, a "#"
// comment inside a ```code block``` is no longer mistaken for a heading.
package markdown

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Section is a Markdown heading and a count of its concrete body content
// (list items, table rows, and substantial paragraphs) up to the next heading of
// the same or higher level — sub-headings and their content are included.
type Section struct {
	Level int
	Title string
	Units int
}

// Doc is the parsed view of a SKILL.md body.
type Doc struct {
	Sections       []Section
	Prose          string   // body text with code blocks and code spans blanked
	Links          []string // link destinations and code-span contents (ref candidates)
	HasOrderedList bool
}

// Parse parses a Markdown body. It is pure: same input, same Doc. GFM is enabled
// so tables are recognized.
func Parse(body string) *Doc {
	src := []byte(body)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	root := md.Parser().Parse(text.NewReader(src))
	blocks := children(root)
	return &Doc{
		Sections:       sections(blocks, src),
		Prose:          prose(root, src),
		Links:          links(root, src),
		HasOrderedList: hasOrderedList(root),
	}
}

func children(root ast.Node) []ast.Node {
	var out []ast.Node
	for c := root.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, c)
	}
	return out
}

// sections builds one Section per heading, counting content units until the next
// heading of the same or higher level.
func sections(blocks []ast.Node, src []byte) []Section {
	var out []Section
	for i, n := range blocks {
		h, ok := n.(*ast.Heading)
		if !ok {
			continue
		}
		units := 0
		for j := i + 1; j < len(blocks); j++ {
			if h2, ok := blocks[j].(*ast.Heading); ok && h2.Level <= h.Level {
				break
			}
			units += unitCount(blocks[j], src)
		}
		out = append(out, Section{Level: h.Level, Title: nodeText(n, src), Units: units})
	}
	return out
}

// unitCount returns how many concrete content points a block contributes: one per
// list item, one per table row, and one for a paragraph with substantial text.
func unitCount(n ast.Node, src []byte) int {
	switch n.Kind() {
	case ast.KindList:
		return countChildren(n, ast.KindListItem)
	case extast.KindTable:
		return countChildren(n, extast.KindTableRow) + countChildren(n, extast.KindTableHeader)
	case ast.KindParagraph, ast.KindTextBlock:
		if len([]rune(nodeText(n, src))) > 20 {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func countChildren(n ast.Node, kind ast.NodeKind) int {
	count := 0
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Kind() == kind {
			count++
		}
	}
	return count
}

// hasOrderedList reports whether the document contains any ordered list (a signal
// of a step-by-step workflow).
func hasOrderedList(root ast.Node) bool {
	found := false
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if l, ok := n.(*ast.List); ok && l.IsOrdered() {
				found = true
				return ast.WalkStop, nil
			}
		}
		return ast.WalkContinue, nil
	})
	return found
}

// links collects Markdown link destinations and code-span contents — the
// candidate resource references a skill may point at.
func links(root ast.Node, src []byte) []string {
	var out []string
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindLink:
			out = append(out, string(n.(*ast.Link).Destination))
		case ast.KindCodeSpan:
			out = append(out, nodeText(n, src))
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return out
}

// prose returns the body with the contents of code blocks and code spans blanked
// (replaced by spaces, newlines preserved), so prose-quality checks do not count
// phrases inside code examples or backtick-quoted anti-patterns.
func prose(root ast.Node, src []byte) string {
	buf := append([]byte(nil), src...)
	blank := func(start, stop int) {
		for i := start; i < stop && i < len(buf); i++ {
			if buf[i] != '\n' {
				buf[i] = ' '
			}
		}
	}
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindFencedCodeBlock, ast.KindCodeBlock:
			lines := n.Lines()
			for i := range lines.Len() {
				seg := lines.At(i)
				blank(seg.Start, seg.Stop)
			}
		case ast.KindCodeSpan:
			blankCodeSpan(n, blank)
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return string(buf)
}

func blankCodeSpan(n ast.Node, blank func(start, stop int)) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			blank(t.Segment.Start, t.Segment.Stop)
		}
	}
}

// nodeText concatenates the readable text of a subtree: Text/String literals and
// code-span contents. Used for heading titles and paragraph length.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := node.(type) {
		case *ast.Text:
			b.Write(v.Segment.Value(src))
		case *ast.String:
			b.Write(v.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}
