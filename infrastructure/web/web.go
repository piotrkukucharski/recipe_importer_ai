package web

import (
	"embed"
	"html/template"
	"io"
	"runtime/debug"

	"github.com/labstack/echo/v4"
)

//go:embed templates/*
var templatesFS embed.FS

var Templates = template.Must(template.ParseFS(templatesFS, "templates/index.html", "templates/progress.html", "templates/imports.html", "templates/tools.html"))

type TemplateRenderer struct {
	Templates *template.Template
}

func NewTemplateRenderer() *TemplateRenderer {
	return &TemplateRenderer{Templates: Templates}
}

func (t *TemplateRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.Templates.ExecuteTemplate(w, name, data)
}

var (
	VersionBranch    = "unknown"
	VersionTag       = ""
	VersionCommit    = "unknown"
	VersionBuildDate = "unknown"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if VersionCommit == "unknown" {
					VersionCommit = setting.Value
				}
			case "vcs.time":
				if VersionBuildDate == "unknown" {
					VersionBuildDate = setting.Value
				}
			}
		}
	}
}

func GetTemplateData(extra map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"VersionBranch":    VersionBranch,
		"VersionTag":       VersionTag,
		"VersionCommit":    VersionCommit,
		"VersionBuildDate": VersionBuildDate,
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}
