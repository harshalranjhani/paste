package app

import (
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

func (s *Server) writePasteViewer(w http.ResponseWriter, sess *sessionUser, paste *pasteRow, files []pasteFileMeta, selected *pasteFileRow) {
	viewURL := "/p/" + paste.PublicID
	pasteURL := s.cfg.BaseURL + viewURL
	rawURL := "/api/v1/pastes/" + paste.PublicID + "/files/" + strconv.FormatInt(selected.ID, 10) + "/raw"
	title := "Untitled paste"
	if len(files) == 1 {
		title = selected.Path
	}
	if paste.Title.Valid && paste.Title.String != "" {
		title = paste.Title.String
	}
	totalBytes := 0
	var options strings.Builder
	for _, file := range files {
		totalBytes += file.SizeBytes
		current := ""
		if file.ID == selected.ID {
			current = " selected"
		}
		options.WriteString(`<option value="` + viewURL + `?file_id=` + strconv.FormatInt(file.ID, 10) + `"` + current + `>` + html.EscapeString(file.Path) + `</option>`)
	}
	actions := `<div class="viewer-actions"><a class="button" href="` + viewURL + `/archive.zip">` + icon("download") + `Download ZIP</a><button class="button button-primary" type="button" id="copy-url" data-copy="` + html.EscapeString(pasteURL) + `">` + icon("copy") + `Copy link</button></div>`
	heading := pageHeading("Shared workspace / "+paste.PublicID, title, "", actions)
	meta := `<div class="mb-6 flex flex-wrap items-center gap-3 text-xs text-muted">` + pasteBadge(paste.ProtectionMode, paste.ExpiresAt) + `<span>` + strconv.Itoa(len(files)) + ` files</span><span>` + formatBytes(totalBytes) + `</span><span>Expires ` + displayTime(paste.ExpiresAt) + `</span></div>`
	sidebar, picker, layout := "", "", "card"
	if len(files) > 1 {
		layout += " viewer-layout"
		sidebar = `<aside class="file-sidebar"><div class="sidebar-title"><span>Files</span><span class="badge">` + strconv.Itoa(len(files)) + `</span></div><ul class="file-tree" id="file-tree" role="tree" aria-label="Paste files">` + renderFileTree(files, selected.ID, viewURL) + `</ul><p class="tree-help">Arrow keys to browse · Enter to open</p></aside>`
		picker = `<label class="file-picker">Select file<select id="file-picker">` + options.String() + `</select></label>`
	}
	toolbar := `<div class="code-toolbar"><div class="code-filename">` + icon("file") + `<span id="selected-path">` + html.EscapeString(selected.Path) + `</span></div><div class="viewer-actions"><button class="button button-ghost" type="button" id="wrap-lines" aria-pressed="false">Wrap lines</button><a class="button" id="raw-link" href="` + rawURL + `">Raw</a><button class="button" id="copy-contents" type="button" data-copy-file="` + rawURL + `">` + icon("copy") + `Copy contents</button></div></div>`
	code := `<div class="code-pane" id="code-pane" aria-label="File contents">` + highlightCode(selected.Path, selected.Content) + `</div>`
	bottom := `<div class="viewer-bottom"><span>Read only · Syntax highlighted</span><span id="file-size">` + formatBytes(len(selected.Content)) + `</span></div>`
	writePage(w, title, "", sess, `<div class="viewer-heading">`+heading+`</div>`+meta+picker+`<div class="`+layout+`">`+sidebar+`<section class="code-main" data-view-url="`+viewURL+`">`+toolbar+code+bottom+`</section></div>`)
}

type fileTreeNode struct {
	name     string
	file     *pasteFileMeta
	children map[string]*fileTreeNode
}

func renderFileTree(files []pasteFileMeta, selectedID int64, viewURL string) string {
	root := &fileTreeNode{children: make(map[string]*fileTreeNode)}
	for i := range files {
		node := root
		for _, part := range strings.Split(files[i].Path, "/") {
			if node.children[part] == nil {
				node.children[part] = &fileTreeNode{name: part, children: make(map[string]*fileTreeNode)}
			}
			node = node.children[part]
		}
		node.file = &files[i]
	}
	return renderTreeChildren(root, selectedID, viewURL)
}

func renderTreeChildren(parent *fileTreeNode, selectedID int64, viewURL string) string {
	nodes := make([]*fileTreeNode, 0, len(parent.children))
	for _, node := range parent.children {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if (len(nodes[i].children) > 0) != (len(nodes[j].children) > 0) {
			return len(nodes[i].children) > 0
		}
		return nodes[i].name < nodes[j].name
	})
	var out strings.Builder
	for _, node := range nodes {
		if len(node.children) > 0 {
			out.WriteString(`<li role="none"><details open><summary role="treeitem" aria-expanded="true" tabindex="-1">` + strings.Replace(icon("chevron"), `class="icon"`, `class="icon chevron"`, 1) + icon("folder") + `<span>` + html.EscapeString(node.name) + `</span></summary><ul role="group">` + renderTreeChildren(node, selectedID, viewURL) + `</ul></details></li>`)
		}
		if node.file != nil {
			file := node.file
			current := ` aria-selected="false" tabindex="-1"`
			if file.ID == selectedID {
				current = ` aria-selected="true" aria-current="page" tabindex="0"`
			}
			out.WriteString(`<li role="none"><a class="file-link" role="treeitem" href="` + viewURL + `?file_id=` + strconv.FormatInt(file.ID, 10) + `" data-path="` + html.EscapeString(file.Path) + `" data-file-id="` + strconv.FormatInt(file.ID, 10) + `" data-size="` + formatBytes(file.SizeBytes) + `"` + current + ` title="` + html.EscapeString(file.Path) + `">` + icon("file") + `<span>` + html.EscapeString(node.name) + `</span></a></li>`)
		}
	}
	return out.String()
}

func formatBytes(size int) string {
	if size < 1024 {
		return strconv.Itoa(size) + " B"
	}
	if size < 1024*1024 {
		return strconv.FormatFloat(float64(size)/1024, 'f', 1, 64) + " KB"
	}
	return strconv.FormatFloat(float64(size)/(1024*1024), 'f', 1, 64) + " MB"
}
