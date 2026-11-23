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
	valid := dto.WalletCreditRequestDTO{Currency: "coins", Amount: 50, Reason: " Spin_Wheel "}
	rule, err := valid.Validate()
	if err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if valid.Reason != "spin_wheel" || rule.MaxPerDay <= 0 {
		t.Errorf("expected normalised reason and a daily cap, got %q %+v", valid.Reason, rule)
	}

	invalid := dto.WalletCreditRequestDTO{Currency: "bitcoin", Amount: 10, Reason: "spin_wheel"}
	if _, err := invalid.Validate(); err == nil {
		t.Error("expected error for invalid currency")
	}
}

// Only the in-game rewards the app really awards can be credited, each up to
// its largest prize. Coin packs and anything else paid for must go through
// billing verification, so they are refused here.
func TestWalletCreditRequestDTO_Allowlist(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		currency string
		amount   int
		wantErr  bool
	}{
		{"spin wheel top prize", "spin_wheel", "coins", 100, false},
		{"spin wheel above top prize", "spin_wheel", "coins", 101, true},
		{"golden chest", "chest_reward_golden", "coins", 100, false},
		{"cyber chest fallback", "chest_reward_cyber", "coins", 150, false},
		{"galaxy chest gems", "chest_reward_galaxy", "gems", 25, false},
		{"galaxy chest pays gems, not coins", "chest_reward_galaxy", "coins", 25, true},
		{"galaxy chest above max", "chest_reward_galaxy", "gems", 26, true},
		{"coin pack cannot be self-credited", "shop_coin_pack", "coins", 500, true},
		{"iap cannot be self-credited", "iap_purchase", "coins", 500, true},
		{"free text reason", "Test bonus top-up", "coins", 50, true},
		{"empty reason", "", "coins", 10, true},
		{"a million coins", "spin_wheel", "coins", 1000000, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := dto.WalletCreditRequestDTO{Currency: tt.currency, Amount: tt.amount, Reason: tt.reason}
			_, err := req.Validate()
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
