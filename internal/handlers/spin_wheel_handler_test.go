package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/routes"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type spinEnvelope struct {
	Success bool                `json:"success"`
	Data    dto.SpinResponseDTO `json:"data"`
}

// spinClientID is the throwaway client created by setupSpinTest. Tests in this
// package run sequentially; a dedicated client keeps balance assertions exact
// even while other packages use the shared demo player.
var spinClientID int

// setupSpinTest returns a router and a token for a fresh client (removed by
// cascade afterwards).
func setupSpinTest(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	r, _ := setupWalletTestApp(t)
	if r == nil {
		t.SkipNow()
	}
	routes.RegisterSpinWheelRoutes(r)

	var tableExists bool
	config.DB.Raw("SELECT to_regclass('spin_results') IS NOT NULL").Scan(&tableExists)
	if !tableExists {
		t.Skip("spin wheel migration (000006) not applied")
	}

	tag := strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if err := config.DB.Raw(
		"INSERT INTO clients (username, email, password) VALUES (?, ?, 'x') RETURNING id",
		"spin_"+tag, "spin_"+tag+"@example.test",
	).Scan(&spinClientID).Error; err != nil || spinClientID == 0 {
		t.Fatalf("create test client: %v", err)
	}
	t.Cleanup(func() { config.DB.Exec("DELETE FROM clients WHERE id = ?", spinClientID) })

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"client_id": spinClientID,
		"email":     "spin_" + tag + "@example.test",
		"exp":       time.Now().Add(time.Hour).Unix(),
		"jti":       uuid.NewString(),
	}).SignedString([]byte(config.AppConfig.Server.JWTSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return r, token
}

func doSpin(r *gin.Engine, token, key string) (int, spinEnvelope) {
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/spin-wheel/spin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var env spinEnvelope
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	return w.Code, env
}

func setSpins(n int) { config.DB.Exec("UPDATE clients SET spins = ? WHERE id = ?", n, spinClientID) }

func clientBalances() (coins, gems, spins int) {
	var b struct{ Coins, Gems, Spins int }
	config.DB.Raw("SELECT coins, gems, spins FROM clients WHERE id = ?", spinClientID).Scan(&b)
	return b.Coins, b.Gems, b.Spins
}

func TestSpin_RequiresValidIdempotencyKey(t *testing.T) {
	r, token := setupSpinTest(t)
	setSpins(1)
	for _, key := range []string{"", "not-a-uuid"} {
		if code, _ := doSpin(r, token, key); code != http.StatusBadRequest {
			t.Errorf("key %q: got %d, want 400", key, code)
		}
	}
	if _, _, spins := clientBalances(); spins != 1 {
		t.Errorf("rejected requests must not spend a spin, spins=%d", spins)
	}
}

func TestSpin_NoSpinsIsConflict(t *testing.T) {
	r, token := setupSpinTest(t)
	setSpins(0)
	coins, gems, _ := clientBalances()
	if code, _ := doSpin(r, token, uuid.NewString()); code != http.StatusConflict {
		t.Fatalf("got %d, want 409", code)
	}
	if c, g, _ := clientBalances(); c != coins || g != gems {
		t.Errorf("balances changed without a spin")
	}
}

func TestSpin_PaysPrizeAndReplaysIdempotently(t *testing.T) {
	r, token := setupSpinTest(t)
	setSpins(2)
	coins0, gems0, _ := clientBalances()

	key := uuid.NewString()
	code, first := doSpin(r, token, key)
	if code != http.StatusOK || !first.Success {
		t.Fatalf("spin failed: %d", code)
	}
	if first.Data.SpinsRemaining != 1 || first.Data.Replayed {
		t.Errorf("unexpected first response: %+v", first.Data)
	}
	coins1, gems1, spins1 := clientBalances()
	switch first.Data.Currency {
	case "coins":
		if coins1 != coins0+first.Data.Amount || gems1 != gems0 {
			t.Errorf("coins %d→%d for prize %d", coins0, coins1, first.Data.Amount)
		}
	case "gems":
		if gems1 != gems0+first.Data.Amount || coins1 != coins0 {
			t.Errorf("gems %d→%d for prize %d", gems0, gems1, first.Data.Amount)
		}
	}

	// Retrying the same key returns the same prize and spends/pays nothing more.
	code, again := doSpin(r, token, key)
	if code != http.StatusOK || !again.Data.Replayed {
		t.Fatalf("replay: %d %+v", code, again.Data)
	}
	if again.Data.SegmentCode != first.Data.SegmentCode || again.Data.Amount != first.Data.Amount {
		t.Errorf("replay changed the prize: %+v vs %+v", again.Data, first.Data)
	}
	if c, g, s := clientBalances(); c != coins1 || g != gems1 || s != spins1 {
		t.Errorf("replay changed balances: %d/%d/%d", c, g, s)
	}

	var ledgerRows int
	config.DB.Raw("SELECT count(*) FROM wallet_ledger WHERE client_id = ? AND transaction_type = 'spin_reward' AND reference_id IN (SELECT id::text FROM spin_results WHERE idempotency_key = ?)", spinClientID, key).Scan(&ledgerRows)
	if ledgerRows != 1 {
		t.Errorf("want exactly 1 wallet ledger row, got %d", ledgerRows)
	}
}

func TestSpin_ConcurrentRequestsSpendOnlyOneSpin(t *testing.T) {
	r, token := setupSpinTest(t)
	setSpins(1)
	coins0, gems0, _ := clientBalances()

	const n = 30
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners []dto.SpinResponseDTO
	codes := map[int]int{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, env := doSpin(r, token, uuid.NewString())
			mu.Lock()
			defer mu.Unlock()
			codes[code]++
			if code == http.StatusOK {
				winners = append(winners, env.Data)
			}
		}()
	}
	wg.Wait()

	if codes[http.StatusOK] != 1 || codes[http.StatusConflict] != n-1 {
		t.Fatalf("status counts %v, want 1x200 and %dx409", codes, n-1)
	}
	coins1, gems1, spins1 := clientBalances()
	if spins1 != 0 {
		t.Errorf("spins = %d, want 0", spins1)
	}
	if got := (coins1 - coins0) + (gems1 - gems0); got != winners[0].Amount {
		t.Errorf("paid %d, want exactly one prize of %d", got, winners[0].Amount)
	}
}

func TestSpin_SameKeyRaceSpendsOnce(t *testing.T) {
	r, token := setupSpinTest(t)
	setSpins(5)

	key := uuid.NewString()
	const n = 20
	var wg sync.WaitGroup
	results := make([]dto.SpinResponseDTO, n)
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, env := doSpin(r, token, key)
			codes[i], results[i] = c, env.Data
		}()
	}
	wg.Wait()

	for i := range results {
		if codes[i] != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i, codes[i])
		}
		if results[i].SegmentCode != results[0].SegmentCode {
			t.Errorf("same key produced different prizes: %s vs %s", results[i].SegmentCode, results[0].SegmentCode)
		}
	}
	if _, _, spins := clientBalances(); spins != 4 {
		t.Errorf("spins = %d, want 4 (one spent)", spins)
	}
}

func TestSpin_GrantIsIdempotentPerReference(t *testing.T) {
	_, _ = setupSpinTest(t)
	setSpins(0)
	ref := "test-session-" + uuid.NewString()

	for i, wantApplied := range []bool{true, false, false} {
		applied, err := services.GrantSpins(t.Context(), spinClientID, 2, "game_complete", ref)
		if err != nil {
			t.Fatalf("grant %d: %v", i, err)
		}
		if applied != wantApplied {
			t.Errorf("grant %d applied=%v, want %v", i, applied, wantApplied)
		}
	}
	if _, _, spins := clientBalances(); spins != 2 {
		t.Errorf("spins = %d, want 2 (granted once)", spins)
	}
}

func TestSpinWheel_SegmentsPublishOdds(t *testing.T) {
	r, token := setupSpinTest(t)
	req, _ := http.NewRequest(http.MethodGet, "/spin-wheel/segments", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data []dto.SpinSegmentDTO `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	total, jackpot := 0.0, 0.0
	for _, s := range env.Data {
		total += s.OddsPercent
		if s.IsJackpot {
			jackpot = s.OddsPercent
		}
	}
	if total < 99.999 || total > 100.001 || jackpot != 0.5 {
		t.Errorf("odds total %.4f%%, jackpot %.4f%%", total, jackpot)
	}
}
