package response

import (
	"time"

	"github.com/gin-gonic/gin"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type Response struct {
	Success   bool         `json:"success"`
	Message   string       `json:"message"`
	Data      any          `json:"data,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
	Timestamp string       `json:"timestamp"`
}

func Success(c *gin.Context, status int, message string, data any) {
	c.JSON(status, Response{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func Error(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, Response{
		Success:   false,
		Message:   message,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func ValidationError(c *gin.Context, status int, message string, errors []FieldError) {
	c.AbortWithStatusJSON(status, Response{
		Success:   false,
		Message:   message,
		Errors:    errors,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}
