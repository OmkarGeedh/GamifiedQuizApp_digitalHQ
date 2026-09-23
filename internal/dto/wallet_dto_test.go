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
