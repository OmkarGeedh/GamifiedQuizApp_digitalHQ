package repo_test

import (
	"context"
	"testing"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/testutil"
)

func TestCheckUsernameExists_CaseAndSpaceInsensitive(t *testing.T) {
	db := testutil.NewDB(t)
	testutil.SeedClient(t, db, "Alex")
	db.Exec("UPDATE clients SET status = 'deleted'") // deleted accounts still hold their name

	for _, name := range []string{"Alex", "alex", "ALEX", "  alex "} {
		exists, err := repo.CheckUsernameExists(context.Background(), name)
		if err != nil || !exists {
			t.Errorf("CheckUsernameExists(%q) = %v, %v; want true", name, exists, err)
		}
	}
	if exists, _ := repo.CheckUsernameExists(context.Background(), "Alexa"); exists {
		t.Error("CheckUsernameExists(\"Alexa\") = true; want false")
	}
}
