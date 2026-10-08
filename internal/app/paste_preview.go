package app

import (
	"bytes"
	"html"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var markdownPreview = goldmark.New(goldmark.WithExtensions(extension.GFM))

func filePreviewKind(filename string) string {
	switch strings.ToLower(path.Ext(filename)) {
	case ".md", ".markdown":
		return "markdown"
	case ".html", ".htm":
		return "html"
	default:
		return ""
	}
}

func fileViewMode(filename, requested string) string {
	if requested == "preview" && filePreviewKind(filename) != "" {
		return "preview"
	}
	return "code"
}

func renderFileContents(file *pasteFileRow, language, mode string) string {
	if mode != "preview" {
		return highlightCode(file.Path, file.Content, language)
	}
	kind := filePreviewKind(file.Path)
	content, css := file.Content, "html { color-scheme: light; } body { background: white; color: black; }"
	if kind == "markdown" {
		var rendered bytes.Buffer
		if err := markdownPreview.Convert([]byte(file.Content), &rendered); err != nil {
			return highlightCode(file.Path, file.Content, language)
		}
		content, css = rendered.String(), markdownPreviewCSS
	}
	document := `<!doctype html><html data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; base-uri 'none'; form-action 'none'; style-src 'unsafe-inline'; img-src data:"><style>` + css + `</style></head><body>` + content + `</body></html>`
	return `<iframe class="file-preview" title="Preview of ` + html.EscapeString(file.Path) + `" sandbox="" referrerpolicy="no-referrer" data-preview-kind="` + kind + `" srcdoc="` + html.EscapeString(document) + `"></iframe>`
}

const markdownPreviewCSS = `
:root { color-scheme: light; --ink: #1d2b27; --paper: #fff; --muted: #626e68; --line: #e1e7e1; --subtle: #f7f9f7; --link: #426433; }
:root[data-theme="dark"] { color-scheme: dark; --ink: #e6ede8; --paper: #18211c; --muted: #a3b2a8; --line: #334239; --subtle: #101613; --link: #b5d89a; }
body { margin: 0 auto; max-width: 56rem; padding: 2rem; font: 15px/1.7 ui-sans-serif, system-ui, sans-serif; color: var(--ink); background: var(--paper); overflow-wrap: anywhere; }
h1, h2, h3 { line-height: 1.3; margin: 1.5em 0 .6em; } h1:first-child { margin-top: 0; } h1, h2 { border-bottom: 1px solid var(--line); padding-bottom: .35em; }
a { color: var(--link); } pre, code { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; background: var(--subtle); border-radius: .35rem; }
code { padding: .15em .3em; } pre { overflow: auto; padding: 1rem; } pre code { padding: 0; }
blockquote { margin-left: 0; padding-left: 1rem; border-left: 3px solid var(--line); color: var(--muted); }
table { border-collapse: collapse; display: block; overflow: auto; } th, td { border: 1px solid var(--line); padding: .5rem .8rem; } th { background: var(--subtle); }
img { max-width: 100%; } hr { border: 0; border-top: 1px solid var(--line); } input { accent-color: var(--link); }
@media (max-width: 600px) { body { padding: 1rem; } }
`
