// Package pages serves the public web pages Google Play requires for the
// listing: the privacy policy and how to delete an account without the app.
package pages

import (
	"embed"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed templates/*.html
var files embed.FS

var templates = template.Must(template.ParseFS(files, "templates/*.html"))

// Updated is the date the policy text last changed.
const Updated = "23 November 2026"

type pageData struct {
	SupportEmail string
	Updated      string
}

func serve(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "public, max-age=3600")
		c.Status(http.StatusOK)
		// html/template escapes the email; it comes from server config only.
		_ = templates.ExecuteTemplate(c.Writer, name, pageData{
			SupportEmail: strings.TrimSpace(os.Getenv("SUPPORT_EMAIL")),
			Updated:      Updated,
		})
	}
}

// Register serves /privacy and /account-deletion under the root and /api/v1.
func Register(router *gin.Engine) {
	for _, prefix := range []string{"", "/api/v1"} {
		router.GET(prefix+"/privacy", serve("privacy.html"))
		router.GET(prefix+"/account-deletion", serve("account-deletion.html"))
	}
}
