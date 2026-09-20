package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/gin-gonic/gin"
)

func TestGetProfileOptions_PublicEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes.RegisterProfileRoutes(router)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/profile/options", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool                       `json:"success"`
		Data    dto.ProfileSetupOptionsDTO `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse options response: %v", err)
	}

	if !resp.Success {
		t.Errorf("Expected success = true")
	}
	if len(resp.Data.Avatars) == 0 {
		t.Errorf("Expected non-empty avatars list")
	}
	if len(resp.Data.Classes) == 0 {
		t.Errorf("Expected non-empty classes list")
	}
	if len(resp.Data.Boards) == 0 {
		t.Errorf("Expected non-empty boards list")
	}
	if len(resp.Data.Subjects) == 0 {
		t.Errorf("Expected non-empty subjects list")
	}
}

