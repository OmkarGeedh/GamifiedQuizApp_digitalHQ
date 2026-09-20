package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
)

// DebitWallet deducts currency from the player's wallet with atomic validation.
func DebitWallet(ctx context.Context, clientID int, req *dto.WalletDebitRequestDTO) (*dto.WalletTransactionDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

	entry, balanceAfter, err := repo.DebitWalletBalance(ctx, clientID, req.Currency, req.Amount, req.Reason, req.ReferenceID)
	if err != nil {
		if errors.Is(err, repo.ErrInsufficientFunds) {
			return nil, http.StatusBadRequest, fmt.Errorf("not enough %s", req.Currency)
		}
		if errors.Is(err, repo.ErrProfileNotFound) {
			// Ensure profile exists then retry once
			if _, pErr := GetOrCreateProfile(ctx, clientID); pErr == nil {
				entry, balanceAfter, err = repo.DebitWalletBalance(ctx, clientID, req.Currency, req.Amount, req.Reason, req.ReferenceID)
			}
		}
		if err != nil {
			if errors.Is(err, repo.ErrInsufficientFunds) {
				return nil, http.StatusBadRequest, fmt.Errorf("not enough %s", req.Currency)
			}
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to process debit: %w", err)
		}
	}

	res := &dto.WalletTransactionDTO{
		ID:           entry.ID,
		UserID:       strconv.Itoa(clientID),
		Currency:     entry.Currency,
		Direction:    entry.Direction,
		Amount:       entry.Amount,
		Reason:       req.Reason,
		BalanceAfter: balanceAfter,
		CreatedAt:    entry.CreatedAt,
	}

	return res, http.StatusOK, nil
}

// CreditWallet adds currency to the player's wallet and records an audit log.
func CreditWallet(ctx context.Context, clientID int, req *dto.WalletCreditRequestDTO) (*dto.WalletTransactionDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}

	entry, balanceAfter, err := repo.CreditWalletBalance(ctx, clientID, req.Currency, req.Amount, req.Reason, req.ReferenceID)
	if err != nil {
		if errors.Is(err, repo.ErrProfileNotFound) {
			if _, pErr := GetOrCreateProfile(ctx, clientID); pErr == nil {
				entry, balanceAfter, err = repo.CreditWalletBalance(ctx, clientID, req.Currency, req.Amount, req.Reason, req.ReferenceID)
			}
		}
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to process credit: %w", err)
		}
	}

	res := &dto.WalletTransactionDTO{
		ID:           entry.ID,
		UserID:       strconv.Itoa(clientID),
		Currency:     entry.Currency,
		Direction:    entry.Direction,
		Amount:       entry.Amount,
		Reason:       req.Reason,
		BalanceAfter: balanceAfter,
		CreatedAt:    entry.CreatedAt,
	}

	return res, http.StatusOK, nil
}

// GetWalletBalance returns current coins and gems for a client.
func GetWalletBalance(ctx context.Context, clientID int) (*dto.WalletBalanceDTO, int, error) {
	coins, gems, err := repo.GetWalletBalance(ctx, clientID)
	if err != nil {
		if errors.Is(err, repo.ErrProfileNotFound) {
			p, pErr := GetOrCreateProfile(ctx, clientID)
			if pErr != nil {
				return nil, http.StatusInternalServerError, fmt.Errorf("failed to initialize profile: %w", pErr)
			}
			return &dto.WalletBalanceDTO{Coins: p.Coins, Gems: p.Gems}, http.StatusOK, nil
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch wallet balance: %w", err)
	}

	return &dto.WalletBalanceDTO{
		Coins: coins,
		Gems:  gems,
	}, http.StatusOK, nil
}

// GetWalletTransactions returns paginated audit records for a client.
func GetWalletTransactions(ctx context.Context, clientID int, limit, offset int) (*dto.WalletHistoryResponseDTO, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	entries, total, err := repo.GetWalletLedgerHistory(ctx, clientID, limit, offset)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to fetch transactions: %w", err)
	}

	dtos := make([]dto.WalletTransactionDTO, 0, len(entries))
	for _, e := range entries {
		dtos = append(dtos, dto.WalletTransactionDTO{
			ID:           e.ID,
			UserID:       strconv.Itoa(clientID),
			Currency:     e.Currency,
			Direction:    e.Direction,
			Amount:       e.Amount,
			Reason:       e.TransactionType,
			BalanceAfter: e.BalanceAfter,
			CreatedAt:    e.CreatedAt,
		})
	}

	return &dto.WalletHistoryResponseDTO{
		Transactions: dtos,
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	}, http.StatusOK, nil
}
