package mailer

import (
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
)

func SendRegistrationOTP(recipient, otp string) error {
	if config.AppConfig == nil {
		return fmt.Errorf("application configuration is unavailable")
	}

	cfg := config.AppConfig.SMTP
	if cfg.Host == "" || cfg.Port == "" || cfg.Username == "" || cfg.Password == "" || cfg.From == "" {
		return fmt.Errorf("SMTP email delivery is not configured")
	}
	if strings.ContainsAny(recipient, "\r\n") {
		return fmt.Errorf("invalid recipient email")
	}
	if _, err := mail.ParseAddress(recipient); err != nil {
		return fmt.Errorf("invalid recipient email: %w", err)
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return fmt.Errorf("invalid sender email: %w", err)
	}

	fromName := mime.QEncoding.Encode("utf-8", cfg.FromName)
	message := []byte(fmt.Sprintf(
		"From: %s <%s>\r\n"+
			"To: %s\r\n"+
			"Subject: Verify your Skillverse email\r\n"+
			"MIME-Version: 1.0\r\n"+
			"Content-Type: text/plain; charset=UTF-8\r\n"+
			"\r\n"+
			"Your Skillverse verification code is %s.\r\n\r\n"+
			"This code expires in 5 minutes. If you did not request it, you can ignore this email.\r\n",
		fromName,
		cfg.From,
		recipient,
		otp,
	))

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	if err := smtp.SendMail(net.JoinHostPort(cfg.Host, cfg.Port), auth, cfg.From, []string{recipient}, message); err != nil {
		return fmt.Errorf("send registration email: %w", err)
	}
	return nil
}
