package dto_test

import (
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
)

func TestWalletDebitRequestDTO_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     dto.WalletDebitRequestDTO
		wantErr bool
	}{
		{
			name: "valid coins debit",
			req: dto.WalletDebitRequestDTO{
				Currency: "coins",
				Amount:   20,
				Reason:   "50:50 powerup",
			},
			wantErr: false,
		},
		{
			name: "valid gems debit with trimming",
			req: dto.WalletDebitRequestDTO{
				Currency: " GEMS ",
				Amount:   5,
			},
			wantErr: false,
		},
		{
			name: "invalid currency",
			req: dto.WalletDebitRequestDTO{
				Currency: "dollars",
				Amount:   10,
			},
			wantErr: true,
		},
		{
			name: "zero amount",
			req: dto.WalletDebitRequestDTO{
				Currency: "coins",
				Amount:   0,
			},
			wantErr: true,
		},
		{
			name: "negative amount",
			req: dto.WalletDebitRequestDTO{
				Currency: "coins",
				Amount:   -5,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWalletCreditRequestDTO_Validate(t *testing.T) {
	valid := dto.WalletCreditRequestDTO{
		Currency: "coins",
		Amount:   50,
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
	if valid.Reason != "Wallet credit" {
		t.Errorf("expected default reason 'Wallet credit', got '%s'", valid.Reason)
	}

	invalid := dto.WalletCreditRequestDTO{
		Currency: "bitcoin",
		Amount:   10,
	}
	if err := invalid.Validate(); err == nil {
		t.Error("expected error for invalid currency")
	}
}

// A credit above the ceiling is refused, while everything the app really sends
// (spin and chest prizes, coin packs, ad gems) still goes through.
func TestWalletCreditRequestDTO_Ceiling(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		amount   int
		wantErr  bool
	}{
		{"largest coin pack the app sells", "coins", 600, false},
		{"jackpot-sized prize", "coins", 100, false},
		{"galaxy chest gems", "gems", 25, false},
		{"exactly the coin ceiling", "coins", dto.MaxWalletCreditCoins, false},
		{"one over the coin ceiling", "coins", dto.MaxWalletCreditCoins + 1, true},
		{"a million coins", "coins", 1000000, true},
		{"exactly the gem ceiling", "gems", dto.MaxWalletCreditGems, false},
		{"one over the gem ceiling", "gems", dto.MaxWalletCreditGems + 1, true},
		{"gems are limited by the gem ceiling, not the coin one", "gems", 1000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := dto.WalletCreditRequestDTO{Currency: tt.currency, Amount: tt.amount}
			err := req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Taking coins away is not capped: that only ever costs the player.
func TestWalletDebitRequestDTO_NoCeiling(t *testing.T) {
	req := dto.WalletDebitRequestDTO{Currency: "coins", Amount: 1000000}
	if err := req.Validate(); err != nil {
		t.Errorf("a large debit should still validate (the balance check refuses it), got %v", err)
	}
}
