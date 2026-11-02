package models

import "time"

// SpinWheelSegment is one prize on the wheel. Odds are WeightPPM parts per million
// (5000 = 0.5%); the active segments' weights always total 1,000,000.
// The table is content owned by SkillVerse-SQL_Migration and read-only here.
type SpinWheelSegment struct {
	SegmentCode string `gorm:"column:segment_code;primaryKey" json:"segment_code"`
	Currency    string `gorm:"column:currency" json:"currency"`
	Amount      int    `gorm:"column:amount" json:"amount"`
	WeightPPM   int    `gorm:"column:weight_ppm" json:"weight_ppm"`
	IsJackpot   bool   `gorm:"column:is_jackpot" json:"is_jackpot"`
	SortOrder   int    `gorm:"column:sort_order" json:"sort_order"`
	Active      bool   `gorm:"column:active" json:"active"`
}

func (SpinWheelSegment) TableName() string { return "spin_wheel_segments" }

// SpinResult is the stored outcome of one spin (amount/currency as paid at the time).
type SpinResult struct {
	ID             string    `gorm:"column:id;primaryKey" json:"id"`
	ClientID       int       `gorm:"column:client_id" json:"-"`
	IdempotencyKey string    `gorm:"column:idempotency_key" json:"-"`
	SegmentCode    string    `gorm:"column:segment_code" json:"segment_code"`
	Currency       string    `gorm:"column:currency" json:"currency"`
	Amount         int       `gorm:"column:amount" json:"amount"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
}

func (SpinResult) TableName() string { return "spin_results" }

// SpinWheelTotalPPM is the required sum of active segment weights.
const SpinWheelTotalPPM = 1_000_000

// Spin ledger reasons.
const SpinReasonSpend = "spin"
