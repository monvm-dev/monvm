// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// pageData contains the values rendered into one documentation page.
type pageData struct {
	Title, Stylesheet string
	Header            template.HTML
	Body              template.HTML
}

// generatedHTMLHeader preserves project ownership in generated pages.
const generatedHTMLHeader = "<!-- MonVM <https://monvm.dev> | Copyright The MonVM Authors | SPDX-License-Identifier: Apache-2.0 -->"

// main renders the documentation website or exits with an error.
func main() {
	out := flag.String("out", "dist/website", "output directory")
	flag.Parse()
	if err := build(*out); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// build recreates the complete documentation website under out.
func build(out string) error {
	files, err := filepath.Glob("docs/*.md")
	if err != nil {
		return err
	}
	sort.Strings(files)
	files = append([]string{"README.md"}, files...)
	if err = os.RemoveAll(out); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(out, "docs"), 0o755); err != nil {
		return err
	}
	for _, name := range []string{"style.css", "favicon.svg"} {
		body, readErr := os.ReadFile(filepath.Join("scripts/sitegen", name))
		if readErr != nil {
			return readErr
		}
		if err = os.WriteFile(filepath.Join(out, name), body, 0o644); err != nil {
			return err
		}
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "LICENSE"), license, 0o644); err != nil {
		return err
	}
	page, err := template.ParseFiles("scripts/sitegen/page.html")
	if err != nil {
		return err
	}
	for _, name := range files {
		if err = render(page, out, name); err != nil {
			return fmt.Errorf("render %s: %w", name, err)
		}
	}
	return nil
}

// render converts one Markdown source file into an HTML page.
func render(page *template.Template, out, name string) error {
	source, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	markdown := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err = markdown.Convert(source, &body); err != nil {
		return err
	}
	title := "MonVM"
	for _, line := range strings.Split(string(source), "\n") {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# ")) + " · MonVM"
			break
		}
	}
	destination, prefix := "index.html", ""
	if name != "README.md" {
		destination, prefix = strings.TrimSuffix(name, ".md")+".html", "../"
	}
	content := strings.ReplaceAll(body.String(), ".md\"", ".html\"")
	var result bytes.Buffer
	data := pageData{Title: title, Stylesheet: prefix + "style.css", Header: template.HTML(generatedHTMLHeader), Body: template.HTML(content)}
	if err = page.Execute(&result, data); err != nil {
		return err
	}
	path := filepath.Join(out, destination)
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, result.Bytes(), 0o644)
}
