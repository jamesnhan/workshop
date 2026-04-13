package testhelpers_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/require"
)

// Smoke test: the test infra itself works end-to-end. This is the first
// test in the repo and exists mostly to prove the toolchain is wired up.
func TestTempDB_OpenCreateList(t *testing.T) {
	d := testhelpers.TempDB(t)

	card := &db.Card{Title: "smoke", Column: "backlog", Project: "workshop-test"}
	require.NoError(t, d.CreateCard(card))
	require.NotZero(t, card.ID)

	got, err := d.ListCards("workshop-test")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "smoke", got[0].Title)
}
