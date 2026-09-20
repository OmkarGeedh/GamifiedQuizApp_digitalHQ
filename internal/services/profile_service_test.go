package services_test

import (
	"context"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
)

func TestGetProfileSetupOptions(t *testing.T) {
	ctx := context.Background()
	opts, status, err := services.GetProfileSetupOptions(ctx)
	if err != nil {
		t.Fatalf("GetProfileSetupOptions failed with err: %v", err)
	}
	if status != 200 {
		t.Errorf("Expected status 200, got %d", status)
	}

	// 1. Verify Avatars (matches Screen 1)
	if len(opts.Avatars) < 6 {
		t.Errorf("Expected at least 6 avatars, got %d", len(opts.Avatars))
	}
	avatarMap := make(map[string]bool)
	for _, a := range opts.Avatars {
		avatarMap[a.ID] = true
	}
	for _, expectedID := range []string{"user", "paw", "moon", "robot", "ninja", "magic"} {
		if !avatarMap[expectedID] {
			t.Errorf("Expected avatar %s in options", expectedID)
		}
	}

	// 2. Verify Classes (matches Screen 2)
	classMap := make(map[string]bool)
	for _, c := range opts.Classes {
		classMap[c] = true
	}
	if !classMap["11th"] {
		t.Errorf("Expected '11th' in classes")
	}

	// 3. Verify Boards (matches Screen 2)
	boardMap := make(map[string]bool)
	for _, b := range opts.Boards {
		boardMap[b] = true
	}
	if !boardMap["Maharashtra State Board"] {
		t.Errorf("Expected 'Maharashtra State Board' in boards")
	}

	// 4. Verify Subjects (matches Screen 3)
	if len(opts.Subjects) == 0 {
		t.Errorf("Expected at least 1 subject in options")
	}
	hasAccounting := false
	for _, s := range opts.Subjects {
		if s.ID == "accounting" && s.Name == "Book-Keeping & Accountancy" {
			hasAccounting = true
			if s.Difficulty != "Beginner" {
				t.Errorf("Expected difficulty 'Beginner', got %s", s.Difficulty)
			}
			if s.TopicsCount != 1 {
				t.Errorf("Expected topicsCount=1, got %d", s.TopicsCount)
			}
		}
	}
	if !hasAccounting {
		t.Errorf("Expected 'Book-Keeping & Accountancy' (id: accounting) in subjects")
	}
}
