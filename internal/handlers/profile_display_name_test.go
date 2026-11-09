package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/handlers"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const takenMessage = "That display name is already taken."

// newNameRouter wires the real handlers behind a stub auth middleware that trusts X-Test-Client.
func newNameRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		id, _ := strconv.Atoi(c.GetHeader("X-Test-Client"))
		c.Set("client_id", id)
		c.Next()
	})
	r.GET("/session", handlers.ClientSessionHandler)
	r.GET("/profile", handlers.GetProfileHandler)
	r.PUT("/profile", handlers.UpdateProfileHandler)
	r.POST("/profile/setup", handlers.SetupProfileHandler)
	return r
}

type envelope struct {
	Success bool                       `json:"success"`
	Message string                     `json:"message"`
	Data    map[string]json.RawMessage `json:"data"`
	Errors  []map[string]string        `json:"errors"`
	raw     map[string]json.RawMessage
}

func call(t *testing.T, r *gin.Engine, method, path string, clientID int, body any) (int, envelope) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Client", strconv.Itoa(clientID))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s %s: bad JSON %q: %v", method, path, w.Body.String(), err)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env.raw)
	return w.Code, env
}

func str(t *testing.T, env envelope, key string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(env.Data[key], &s); err != nil {
		t.Fatalf("data.%s is not a string: %s", key, env.Data[key])
	}
	return s
}

// names returns what the session endpoint ("username") and GET /profile ("name") report.
func names(t *testing.T, r *gin.Engine, clientID int) (username, profileName string) {
	t.Helper()
	code, sess := call(t, r, http.MethodGet, "/session", clientID, nil)
	if code != http.StatusOK {
		t.Fatalf("session: status %d", code)
	}
	code, prof := call(t, r, http.MethodGet, "/profile", clientID, nil)
	if code != http.StatusOK {
		t.Fatalf("profile: status %d", code)
	}
	return str(t, sess, "username"), str(t, prof, "name")
}

func assertName(t *testing.T, r *gin.Engine, clientID int, want string) {
	t.Helper()
	u, p := names(t, r, clientID)
	if u != want || p != want {
		t.Fatalf("client %d: session username=%q, profile name=%q; want both %q", clientID, u, p, want)
	}
}

func assertTaken(t *testing.T, code int, env envelope) {
	t.Helper()
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", code)
	}
	if env.Success || env.Message != takenMessage {
		t.Fatalf("success=%v message=%q", env.Success, env.Message)
	}
	if d, ok := env.raw["data"]; !ok || string(d) != "null" {
		t.Fatalf(`"data" must be present and null, got %s (present=%v)`, d, ok)
	}
	if len(env.Errors) != 1 || env.Errors[0]["field"] != "name" || env.Errors[0]["code"] != "display_name_taken" {
		t.Fatalf("errors = %v", env.Errors)
	}
}

func TestUpdateProfileName_FreeNameSavedAndVisibleEverywhere(t *testing.T) {
	db := testutil.NewDB(t)
	r := newNameRouter()
	id := testutil.SeedClient(t, db, "oldname")

	code, env := call(t, r, http.MethodPut, "/profile", id, map[string]any{"name": "  Brand New  "})
	if code != http.StatusOK || !env.Success {
		t.Fatalf("status = %d, body ok=%v", code, env.Success)
	}
	if got := str(t, env, "name"); got != "Brand New" {
		t.Fatalf("response name = %q, want trimmed %q", got, "Brand New")
	}
	// Session and GET /profile both read the stored value, on every call ("after restart").
	assertName(t, r, id, "Brand New")
	assertName(t, r, id, "Brand New")

	var prof models.Profile
	// The profile is the player's clients row, keyed by id.
	if err := db.Where("id = ?", id).First(&prof).Error; err != nil {
		t.Fatal(err)
	}
	if prof.FullName != "Brand New" {
		t.Fatalf("clients.full_name = %q, want kept in sync", prof.FullName)
	}
}

func TestUpdateProfileName_TakenIsConflict(t *testing.T) {
	cases := map[string]string{
		"exact":             "Alex",
		"different case":    "ALEX",
		"surrounding space": "  alex  ",
	}
	for label, attempt := range cases {
		t.Run(label, func(t *testing.T) {
			db := testutil.NewDB(t)
			r := newNameRouter()
			testutil.SeedClient(t, db, "Alex")
			me := testutil.SeedClient(t, db, "Sam")

			// avatarId rides along to prove a rejected request changes nothing.
			code, env := call(t, r, http.MethodPut, "/profile", me, map[string]any{"name": attempt, "avatarId": "ninja"})
			assertTaken(t, code, env)

			assertName(t, r, me, "Sam")
			_, prof := call(t, r, http.MethodGet, "/profile", me, nil)
			if got := str(t, prof, "avatarId"); got != "user" {
				t.Fatalf("avatarId = %q after rejected request, want unchanged %q", got, "user")
			}
		})
	}
}

func TestUpdateProfileName_OwnNameIsNotAConflict(t *testing.T) {
	db := testutil.NewDB(t)
	r := newNameRouter()
	id := testutil.SeedClient(t, db, "Alex")

	if code, _ := call(t, r, http.MethodPut, "/profile", id, map[string]any{"name": "Alex"}); code != http.StatusOK {
		t.Fatalf("re-saving own name: status %d, want 200", code)
	}
	assertName(t, r, id, "Alex")

	if code, _ := call(t, r, http.MethodPut, "/profile", id, map[string]any{"name": " ALEX "}); code != http.StatusOK {
		t.Fatalf("changing only the case of own name: status %d, want 200", code)
	}
	assertName(t, r, id, "ALEX")
}

func TestUpdateProfileName_InvalidIsBadRequest(t *testing.T) {
	db := testutil.NewDB(t)
	r := newNameRouter()
	id := testutil.SeedClient(t, db, "keepme")

	for label, name := range map[string]string{
		"empty":     "",
		"blank":     "    ",
		"too short": "ab",
		"too long":  strings.Repeat("x", 31),
	} {
		t.Run(label, func(t *testing.T) {
			code, env := call(t, r, http.MethodPut, "/profile", id, map[string]any{"name": name})
			if code != http.StatusBadRequest || env.Success {
				t.Fatalf("status = %d, want 400", code)
			}
			if env.Message != "Display name must be 3 to 30 characters." {
				t.Fatalf("message = %q", env.Message)
			}
			assertName(t, r, id, "keepme")
		})
	}

	// Boundaries are inclusive, and length counts characters rather than bytes.
	for _, ok := range []string{"abc", strings.Repeat("x", 30), "Zoë"} {
		if code, _ := call(t, r, http.MethodPut, "/profile", id, map[string]any{"name": ok}); code != http.StatusOK {
			t.Fatalf("name %q: status %d, want 200", ok, code)
		}
	}
}

func TestUpdateProfileName_LegacyOwnNameStillAllowsOtherEdits(t *testing.T) {
	db := testutil.NewDB(t)
	r := newNameRouter()
	id := testutil.SeedClient(t, db, "Jo") // signup allowed 2 characters

	code, _ := call(t, r, http.MethodPut, "/profile", id, map[string]any{"name": "Jo", "avatarId": "robot"})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	assertName(t, r, id, "Jo")
}

func TestUpdateProfileName_ConcurrentRequestsOneWinner(t *testing.T) {
	db := testutil.NewDB(t)
	r := newNameRouter()
	a := testutil.SeedClient(t, db, "playerA")
	b := testutil.SeedClient(t, db, "playerB")

	for round, name := range []string{"Contested", "Another One", "Third Try", "Fourth"} {
		var (
			wg    sync.WaitGroup
			start = make(chan struct{})
			codes = make([]int, 2)
		)
		for i, id := range []int{a, b} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				// Different case/spacing per caller: still the same normalized name.
				attempt := name
				if i == 1 {
					attempt = "  " + strings.ToUpper(name) + " "
				}
				codes[i], _ = call(t, r, http.MethodPut, "/profile", id, map[string]any{"name": attempt})
			}()
		}
		close(start)
		wg.Wait()

		ok, conflict := 0, 0
		for _, c := range codes {
			switch c {
			case http.StatusOK:
				ok++
			case http.StatusConflict:
				conflict++
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("round %d: codes = %v, want exactly one 200 and one 409", round, codes)
		}

		var n int64
		db.Model(&models.Client{}).Where("lower(trim(username)) = lower(trim(?))", name).Count(&n)
		if n != 1 {
			t.Fatalf("round %d: %d rows hold %q, want 1", round, n, name)
		}
		// Reset both so the next round contests a fresh name from a neutral state.
		for _, id := range []int{a, b} {
			db.Session(&gorm.Session{}).Model(&models.Client{}).Where("id = ?", id).Update("username", "p"+strconv.Itoa(id)+"r"+strconv.Itoa(round))
		}
	}
}

func TestSetupProfile_NameUsesSameUniqueness(t *testing.T) {
	db := testutil.NewDB(t)
	r := newNameRouter()
	testutil.SeedClient(t, db, "Taken")
	me := testutil.SeedClient(t, db, "me_user")

	body := map[string]any{"name": "taken", "class": "10th", "board": "CBSE", "subjects": []string{"accounting"}}
	code, env := call(t, r, http.MethodPost, "/profile/setup", me, body)
	assertTaken(t, code, env)
	assertName(t, r, me, "me_user")

	body["name"] = "Fresh Name"
	if code, _ := call(t, r, http.MethodPost, "/profile/setup", me, body); code != http.StatusOK {
		t.Fatalf("setup with free name: status %d, want 200", code)
	}
	assertName(t, r, me, "Fresh Name")
}
