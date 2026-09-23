package mailer

import (
	"strings"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
)

func TestSendRegistrationOTPRequiresSMTPConfiguration(t *testing.T) {
	previousConfig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previousConfig })
	config.AppConfig = &config.Config{}

	err := SendRegistrationOTP("learner@example.com", "123456")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected missing SMTP configuration error, got %v", err)
	}
}

func TestSendRegistrationOTPRejectsInvalidRecipient(t *testing.T) {
	previousConfig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = previousConfig })
	config.AppConfig = &config.Config{
		SMTP: config.SMTPConfig{
			Host:     "smtp.example.com",
			Port:     "587",
			Username: "user",
			Password: "password",
			From:     "no-reply@example.com",
			FromName: "Skillverse",
		},
	}

	err := SendRegistrationOTP("invalid\r\nBcc: attacker@example.com", "123456")
	if err == nil || !strings.Contains(err.Error(), "invalid recipient") {
		t.Fatalf("expected invalid recipient error, got %v", err)
	}
}
