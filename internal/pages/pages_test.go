package pages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPagesAreServedSafely(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r)
	t.Setenv("SUPPORT_EMAIL", `help@example.com"><script>alert(1)</script>`)
	for _, path := range []string{"/privacy", "/api/v1/privacy", "/account-deletion", "/api/v1/account-deletion"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Header().Get("Content-Type"))
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("%s: missing CSP", path)
		}
		if strings.Contains(body, "<script>") {
			t.Errorf("%s: support email was not escaped", path)
		}
		if !strings.Contains(body, "Delete") {
			t.Errorf("%s: deletion instructions missing", path)
		}
	}
}
