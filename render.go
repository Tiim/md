package main

import (
	"bytes"
	"fmt"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/mermaid"
)

var markdown = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		extension.DefinitionList,
		// Mermaid blocks are turned into their own node kind, so the
		// highlighter below never sees them. The page template loads mermaid.js.
		&mermaid.Extender{RenderMode: mermaid.RenderModeClient, NoScript: true},
		highlighting.NewHighlighting(
			highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
		),
	),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

func renderMarkdown(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := markdown.Convert(withFrontmatterDetails(src), &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// withFrontmatterDetails rewrites a leading YAML frontmatter block into a
// collapsed <details> element holding a yaml code block, so it is shown
// highlighted instead of being rendered as a paragraph and a rule.
func withFrontmatterDetails(src []byte) []byte {
	rest, ok := bytes.CutPrefix(bytes.TrimPrefix(src, []byte("\ufeff")), []byte("---"))
	if !ok {
		return src
	}
	rest, ok = cutLineEnd(rest)
	if !ok {
		return src
	}
	var yaml []byte
	for i := 0; i < len(rest); {
		line, _, _ := bytes.Cut(rest[i:], []byte("\n"))
		next := i + len(line) + 1
		if string(bytes.TrimRight(line, " \t\r")) == "---" {
			yaml = rest[:i]
			rest = rest[min(next, len(rest)):]
			break
		}
		i = next
	}
	if yaml == nil {
		return src // no closing delimiter: not frontmatter
	}
	if len(bytes.TrimSpace(yaml)) == 0 {
		return rest
	}

	// The fence must be longer than any backtick run inside the yaml.
	fence := "```"
	for strings.Contains(string(yaml), fence) {
		fence += "`"
	}
	var b bytes.Buffer
	b.WriteString("<details class=\"frontmatter\">\n<summary>Frontmatter</summary>\n\n")
	b.WriteString(fence + "yaml\n")
	b.Write(yaml)
	b.WriteString(fence + "\n\n</details>\n\n")
	b.Write(rest)
	return b.Bytes()
}

// cutLineEnd strips trailing spaces and a line break, reporting whether the
// rest of the line was otherwise empty.
func cutLineEnd(b []byte) ([]byte, bool) {
	b = bytes.TrimLeft(b, " \t")
	b, _ = bytes.CutPrefix(b, []byte("\r"))
	return bytes.CutPrefix(b, []byte("\n"))
}

// chromaCSS returns the syntax highlighting stylesheet for light and dark mode.
// Each style sits in its own exclusive media query: the styles don't define
// the same token classes, so layering dark over light would leak light colors
// (e.g. near-black punctuation) into dark mode.
func chromaCSS() (string, error) {
	f := chromahtml.New(chromahtml.WithClasses(true))
	var b strings.Builder
	for _, theme := range []struct{ media, style string }{
		{"not (prefers-color-scheme: dark)", "github"},
		{"(prefers-color-scheme: dark)", "github-dark"},
	} {
		fmt.Fprintf(&b, "@media %s {\n", theme.media)
		if err := f.WriteCSS(&b, styles.Get(theme.style)); err != nil {
			return "", err
		}
		b.WriteString("}\n")
	}
	return b.String(), nil
}
