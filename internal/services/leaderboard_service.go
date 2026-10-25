package services

import (
	"context"
	"net/http"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// GetLeaderboard validates filters and fetches authoritative ranked leaderboard entries.
func GetLeaderboard(ctx context.Context, currentClientID int, filter *dto.LeaderboardFilterDTO) (*dto.LeaderboardResponseDTO, int, error) {
	if filter == nil {
		filter = &dto.LeaderboardFilterDTO{
			Scope:     dto.ScopeGlobal,
			TimeRange: dto.TimeRangeWeekly,
			Page:      dto.DefaultLeaderboardPage,
			PageSize:  dto.DefaultLeaderboardPageSize,
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

	res, err := repo.GetLeaderboard(ctx, filter, currentClientID)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	return res, http.StatusOK, nil
}
