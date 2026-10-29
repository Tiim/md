package main

import (
	"bytes"
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
	if err := markdown.Convert(src, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// chromaCSS returns the syntax highlighting stylesheet for light and dark mode.
func chromaCSS() (string, error) {
	f := chromahtml.New(chromahtml.WithClasses(true))
	var b strings.Builder
	if err := f.WriteCSS(&b, styles.Get("github")); err != nil {
		return "", err
	}
	b.WriteString("\n@media (prefers-color-scheme: dark) {\n")
	if err := f.WriteCSS(&b, styles.Get("github-dark")); err != nil {
		return "", err
	}
	b.WriteString("}\n")
	return b.String(), nil
}
