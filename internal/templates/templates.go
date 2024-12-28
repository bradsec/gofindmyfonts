// internal/templates/templates.go
package templates

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"sync"
)

//go:embed static/css/* static/js/* html/*
var content embed.FS

var (
	templates     *template.Template
	templatesOnce sync.Once
)

func initTemplates() error {
	// Read CSS and JS
	css, err := content.ReadFile("static/css/styles.css")
	if err != nil {
		return fmt.Errorf("failed to read CSS: %w", err)
	}

	js, err := content.ReadFile("static/js/main.js")
	if err != nil {
		return fmt.Errorf("failed to read JavaScript: %w", err)
	}

	// Create template functions map
	funcMap := template.FuncMap{
		"css": func() template.CSS { return template.CSS(css) },
		"js":  func() template.JS { return template.JS(js) },
	}

	// Parse template with functions
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(content, "html/*.html")
	if err != nil {
		return fmt.Errorf("failed to parse templates: %w", err)
	}

	templates = tmpl
	return nil
}

// RenderIndex renders the index template
func RenderIndex(w io.Writer) error {
	var err error
	templatesOnce.Do(func() {
		err = initTemplates()
	})
	if err != nil {
		return err
	}

	return templates.ExecuteTemplate(w, "index.html", nil)
}

// Add a function to serve the favicon
func ServeFavicon(w io.Writer) error {
	favicon, err := content.ReadFile("static/img/favicon.ico")
	if err != nil {
		return fmt.Errorf("failed to read favicon: %w", err)
	}
	_, err = w.Write(favicon)
	return err
}
