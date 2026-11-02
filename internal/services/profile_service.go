package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"golang.org/x/sync/errgroup"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gorm"
)

// ErrDisplayNameTaken is returned (with HTTP 409) when another user already has the display name.
var ErrDisplayNameTaken = repo.ErrDisplayNameTaken

// saveProfile persists the profile; a non-nil newName is also written to clients.username
// (the value the session endpoint returns) in the same transaction.
func saveProfile(ctx context.Context, profile *models.Profile, newName *string) (int, error) {
	if err := repo.SaveProfileWithDisplayName(ctx, profile, newName); err != nil {
		if errors.Is(err, repo.ErrDisplayNameTaken) {
			return http.StatusConflict, ErrDisplayNameTaken
		}
		return http.StatusInternalServerError, fmt.Errorf("failed to save profile: %w", err)
	}
	return http.StatusOK, nil
}

// CalculateExperienceForLevel returns the total XP required to reach the next level
func CalculateExperienceForLevel(level int) int {
	if level <= 1 {
		return 100
	}
	return level * 150
}

// -----------------------------------------------------------------------------
// DYNAMIC PROFILE SETUP OPTIONS
// -----------------------------------------------------------------------------

// GetProfileSetupOptions returns dynamic options for avatars, classes, boards,
// and curriculum subjects directly from backend data (no frontend hardcoding).
// Assets/icons are rendered and handled directly by the frontend.
func GetProfileSetupOptions(ctx context.Context) (*dto.ProfileSetupOptionsDTO, int, error) {
	avatars := []dto.AvatarOptionDTO{
		{ID: "user", Name: "Classic", Icon: "user"},
		{ID: "paw", Name: "Paw", Icon: "paw"},
		{ID: "moon", Name: "Moon", Icon: "moon"},
		{ID: "robot", Name: "Robot", Icon: "robot"},
		{ID: "ninja", Name: "Ninja", Icon: "ninja"},
		{ID: "magic", Name: "Magic", Icon: "magic"},
	}

	classes := []string{
		"8th",
		"9th",
		"10th",
		"11th",
		"12th",
	}

	boards := []string{
		"Maharashtra State Board",
		"CBSE",
		"ICSE",
		"State Board",
	}

	topicMeta := map[string]struct {
		Name        string
		Description string
		Difficulty  string
	}{
		"accounting": {
			Name:        "Book-Keeping & Accountancy",
			Description: "Available from the deployed backend.",
			Difficulty:  "Beginner",
		},
		"general": {
			Name:        "General Knowledge",
			Description: "Curated general knowledge quizzes.",
			Difficulty:  "All Levels",
		},
	}

	var subjects []dto.SubjectOptionDTO
	topics, err := repo.GetAvailableTopics(ctx)
	if err == nil && len(topics) > 0 {
		for _, t := range topics {
			topicID, _ := t["topic_id"].(string)
			if topicID == "" {
				continue
			}

			meta, exists := topicMeta[topicID]
			if exists {
				subjects = append(subjects, dto.SubjectOptionDTO{
					ID:          topicID,
					Name:        meta.Name,
					Description: meta.Description,
					Difficulty:  meta.Difficulty,
					TopicsCount: 1,
				})
			} else {
				displayName := cases.Title(language.English).String(strings.ReplaceAll(topicID, "_", " "))
				subjects = append(subjects, dto.SubjectOptionDTO{
					ID:          topicID,
					Name:        displayName,
					Description: "Comprehensive topic quizzes available from backend.",
					Difficulty:  "Intermediate",
					TopicsCount: 1,
				})
			}
		}
	}

	// Fallback to Book-Keeping & Accountancy if topics table was not yet populated
	if len(subjects) == 0 {
		subjects = append(subjects, dto.SubjectOptionDTO{
			ID:          "accounting",
			Name:        "Book-Keeping & Accountancy",
			Description: "Available from the deployed backend.",
			Difficulty:  "Beginner",
			TopicsCount: 1,
		})
	}

	return &dto.ProfileSetupOptionsDTO{
		Avatars:  avatars,
		Classes:  classes,
		Boards:   boards,
		Subjects: subjects,
	}, http.StatusOK, nil
}

// -----------------------------------------------------------------------------
// PROFILE RETRIEVAL & INITIALIZATION
// -----------------------------------------------------------------------------

// GetOrCreateProfile returns the player's profile. The profile lives on the clients row
// (created at signup with starter balances), so "create" only fills the display name.
func GetOrCreateProfile(ctx context.Context, clientID int) (*models.Profile, error) {
	profile, err := repo.GetProfileByClientID(ctx, clientID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("client not found: %w", err)
		}
		return nil, err
	}
	now := time.Now().UTC()
	todayStr := now.Format("2006-01-02")
	todayDate, _ := time.Parse("2006-01-02", todayStr)

	if profile.FullName == "" {
		profile.FullName = "Player"
		if client, err := repo.GetClientByID(ctx, clientID); err == nil && client != nil && client.Username != "" {
			profile.FullName = client.Username
		}
		if err := repo.UpdateProfile(ctx, profile); err != nil {
			return nil, fmt.Errorf("failed to initialize profile: %w", err)
		}
	}

	// Profile exists: update streak if new active day
	lastActiveStr := profile.LastActiveDate.Format("2006-01-02")
	if lastActiveStr != todayStr {
		yesterdayStr := now.AddDate(0, 0, -1).Format("2006-01-02")
		if lastActiveStr == yesterdayStr {
			profile.CurrentStreak++
			if profile.CurrentStreak > profile.HighestStreak {
				profile.HighestStreak = profile.CurrentStreak
			}
		} else {
			profile.CurrentStreak = 1
		}

		profile.LastActiveDate = todayDate
		_ = repo.UpdateProfile(ctx, profile)
		_ = repo.RecordDailyActivity(ctx, clientID, todayStr)
	}

	return profile, nil
}

func GetMyProfile(ctx context.Context, clientID int) (*dto.ProfileResponseDTO, int, error) {
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve profile: %w", err)
	}

	var (
		client     *models.Client
		last7Days  []dto.DayStreakDTO
		weeklyRank int
	)

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var cErr error
		client, cErr = repo.GetClientByID(gCtx, clientID)
		return cErr
	})

	g.Go(func() error {
		var sErr error
		last7Days, sErr = repo.GetLast7DaysStreak(gCtx, clientID)
		if sErr != nil {
			return fmt.Errorf("failed to retrieve streak history: %w", sErr)
		}
		return nil
	})

	g.Go(func() error {
		var rErr error
		weeklyRank, rErr = repo.CalculateWeeklyRank(gCtx, profile.WeeklyScore, clientID)
		if rErr != nil {
			weeklyRank = 1
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, http.StatusNotFound, errors.New("client not found")
		}
		return nil, http.StatusInternalServerError, err
	}

	nextLevelExp := CalculateExperienceForLevel(profile.Level)

	stats := &dto.GamificationStatsDTO{
		Coins:               profile.Coins,
		Gems:                profile.Gems,
		Level:               profile.Level,
		Experience:          profile.Experience,
		NextLevelExperience: nextLevelExp,
		WeeklyScore:         profile.WeeklyScore,
		WeeklyRank:          weeklyRank,
	}

	streak := &dto.StreakStatsDTO{
		Current: profile.CurrentStreak,
		Highest: profile.HighestStreak,
		History: last7Days,
	}

	// Parse subjects JSON array
	var subjects []string
	if profile.Subjects != "" {
		_ = json.Unmarshal([]byte(profile.Subjects), &subjects)
	}
	if subjects == nil {
		subjects = []string{}
	}

	avatarID := profile.AvatarID
	if avatarID == "" {
		avatarID = "user"
	}

	res := &dto.ProfileResponseDTO{
		UUID:            profile.UUID,
		Name:            client.Username, // same stored value the session endpoint returns
		Email:           client.Email,
		Phone:           client.Phone,
		AvatarID:        avatarID,
		AvatarURL:       profile.AvatarURL,
		Class:           profile.Class,
		Board:           profile.Board,
		Subjects:        subjects,
		IsOnboarded:     profile.IsOnboarded,
		Streaks:         profile.CurrentStreak,
		HighestStreak:   profile.HighestStreak,
		Last7DaysStreak: last7Days,
		Gems:            profile.Gems,
		Coins:           profile.Coins,
		Level:           profile.Level,
		Experience:      profile.Experience,
		NextLevelExp:    nextLevelExp,
		WeeklyScore:     profile.WeeklyScore,
		WeeklyRank:      weeklyRank,
		Stats:           stats,
		Streak:          streak,
		CreatedAt:       profile.CreatedAt,
		UpdatedAt:       profile.UpdatedAt,
	}

	return res, http.StatusOK, nil
}

// -----------------------------------------------------------------------------
// PROFILE SETUP (4-Step Onboarding Submission)
// -----------------------------------------------------------------------------

// SetupProfile completes the 4-step onboarding flow: saves avatar, name, class,
// board, subjects, and marks is_onboarded = true.
func SetupProfile(ctx context.Context, clientID int, req *dto.ProfileSetupRequestDTO) (*dto.ProfileResponseDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve profile: %w", err)
	}

	profile.AvatarID = req.AvatarID
	if req.AvatarURL != nil && *req.AvatarURL != "" {
		profile.AvatarURL = *req.AvatarURL
	}
	profile.Class = req.Class
	profile.Board = req.Board

	subjectsJSON, err := json.Marshal(req.Subjects)
	if err == nil {
		profile.Subjects = string(subjectsJSON)
	}
	profile.IsOnboarded = true

	if status, err := saveProfile(ctx, profile, &req.Name); err != nil {
		return nil, status, err
	}

	return GetMyProfile(ctx, clientID)
}

// CreateMyProfile is an alias/upsert for SetupProfile for backwards compatibility.
func CreateMyProfile(ctx context.Context, clientID int, req *dto.CreateProfileRequestDTO) (*dto.ProfileResponseDTO, int, error) {
	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to initialize profile: %w", err)
	}

	var newName *string
	if req.FullName != "" {
		newName = &req.FullName
	}
	if req.AvatarID != nil {
		profile.AvatarID = *req.AvatarID
	}
	if req.AvatarURL != nil {
		profile.AvatarURL = *req.AvatarURL
	}
	if req.Class != nil {
		profile.Class = *req.Class
	}
	if req.Board != nil {
		profile.Board = *req.Board
	}
	if len(req.Subjects) > 0 {
		b, _ := json.Marshal(req.Subjects)
		profile.Subjects = string(b)
		profile.IsOnboarded = true
	}

	if status, err := saveProfile(ctx, profile, newName); err != nil {
		return nil, status, err
	}
	if req.Phone != nil {
		_ = repo.UpdateClientPhone(ctx, clientID, req.Phone)
	}

	return GetMyProfile(ctx, clientID)
}

func UpdateMyProfile(ctx context.Context, clientID int, req *dto.UpdateProfileRequestDTO) (*dto.ProfileResponseDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

	// Validate the name before touching any data so a rejected request changes nothing.
	var newName *string
	if req.Name != nil && *req.Name != "" {
		name := *req.Name
		client, err := repo.GetClientByID(ctx, clientID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, http.StatusNotFound, errors.New("client not found")
			}
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve client: %w", err)
		}
		// Re-saving the current name is always allowed, even if it predates the length rule.
		if name != client.Username {
			if err := dto.ValidateDisplayName(name); err != nil {
				return nil, http.StatusBadRequest, err
			}
		}
		newName = &name
	}

	profile, err := GetOrCreateProfile(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to retrieve profile: %w", err)
	}

	if req.AvatarID != nil && *req.AvatarID != "" {
		profile.AvatarID = *req.AvatarID
	}
	if req.AvatarURL != nil {
		profile.AvatarURL = *req.AvatarURL
	}
	if req.Class != nil {
		profile.Class = *req.Class
	}
	if req.Board != nil {
		profile.Board = *req.Board
	}
	if req.Subjects != nil {
		b, _ := json.Marshal(req.Subjects)
		profile.Subjects = string(b)
	}

	if status, err := saveProfile(ctx, profile, newName); err != nil {
		return nil, status, err
	}
	// Phone is best-effort and applied only once the rest of the request has succeeded.
	if req.Phone != nil {
		_ = repo.UpdateClientPhone(ctx, clientID, req.Phone)
	}

	return GetMyProfile(ctx, clientID)
}
