package services

import (
	"math"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func testWheel() []models.SpinWheelSegment {
	return []models.SpinWheelSegment{
		{SegmentCode: "coins-10", Currency: "coins", Amount: 10, WeightPPM: 400000, SortOrder: 1},
		{SegmentCode: "coins-20", Currency: "coins", Amount: 20, WeightPPM: 250000, SortOrder: 2},
		{SegmentCode: "gems-5", Currency: "gems", Amount: 5, WeightPPM: 120000, SortOrder: 3},
		{SegmentCode: "coins-50", Currency: "coins", Amount: 50, WeightPPM: 100000, SortOrder: 4},
		{SegmentCode: "gems-10", Currency: "gems", Amount: 10, WeightPPM: 70000, SortOrder: 5},
		{SegmentCode: "coins-100", Currency: "coins", Amount: 100, WeightPPM: 55000, SortOrder: 6},
		{SegmentCode: "jackpot", Currency: "coins", Amount: 5000, WeightPPM: 5000, SortOrder: 7, IsJackpot: true},
	}
}

func TestPickSpinSegmentBoundaries(t *testing.T) {
	segs := testWheel()
	cases := []struct {
		roll int
		want string
	}{
		{0, "coins-10"},
		{399999, "coins-10"},
		{400000, "coins-20"},
		{994999, "coins-100"},
		{995000, "jackpot"}, // jackpot is exactly the last 5000 of 1,000,000
		{999999, "jackpot"},
	}
	for _, c := range cases {
		if got := PickSpinSegment(segs, c.roll).SegmentCode; got != c.want {
			t.Errorf("roll %d = %s, want %s", c.roll, got, c.want)
		}
	}
}

func TestValidateSpinSegments(t *testing.T) {
	if err := ValidateSpinSegments(testWheel()); err != nil {
		t.Fatalf("valid wheel rejected: %v", err)
	}
	if err := ValidateSpinSegments(nil); err == nil {
		t.Error("empty wheel must be rejected")
	}
	bad := testWheel()
	bad[0].WeightPPM--
	if err := ValidateSpinSegments(bad); err == nil {
		t.Error("weights not totalling 1,000,000 must be rejected")
	}
	bad = testWheel()
	bad[1].Currency = "experience"
	if err := ValidateSpinSegments(bad); err == nil {
		t.Error("unsupported currency must be rejected")
	}
}

func TestSpinRollerDistribution(t *testing.T) {
	segs := testWheel()
	const n = 2_000_000
	counts := map[string]int{}
	for i := 0; i < n; i++ {
		counts[PickSpinSegment(segs, spinRoller(models.SpinWheelTotalPPM)).SegmentCode]++
	}
	// 5 standard deviations of a binomial keeps this test from flaking.
	for _, s := range segs {
		p := float64(s.WeightPPM) / models.SpinWheelTotalPPM
		sd := math.Sqrt(float64(n) * p * (1 - p))
		if diff := math.Abs(float64(counts[s.SegmentCode]) - float64(n)*p); diff > 5*sd {
			t.Errorf("%s: got %d, want %.0f ± %.0f", s.SegmentCode, counts[s.SegmentCode], float64(n)*p, 5*sd)
		}
	}
}
