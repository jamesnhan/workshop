package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// positionOf returns the position of card id in the given column, or -1.
func positionOf(t *testing.T, d *db.DB, id int64) int {
	t.Helper()
	c, err := d.GetCard(id)
	require.NoError(t, err)
	return c.Position
}

// seed creates a handful of root cards in the given column for project p.
// Returns their ids in insertion order.
func seed(t *testing.T, d *db.DB, p, col string, n int) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		c := &db.Card{Title: "seed", Column: col, Project: p, Description: "test card"}
		require.NoError(t, d.CreateCard(c))
		ids = append(ids, c.ID)
	}
	return ids
}

// --- CRUD basics ---

func TestCreateCard_defaultsAndInsert(t *testing.T) {
	d := testhelpers.TempDB(t)

	c := &db.Card{Title: "hello", Column: "backlog", Project: "p"}
	require.NoError(t, d.CreateCard(c))
	require.NotZero(t, c.ID)
	assert.Equal(t, 0, c.Position, "first card in empty column should be at position 0")

	got, err := d.GetCard(c.ID)
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Title)
	assert.Equal(t, "backlog", got.Column)
}

func TestCreateCard_positionMonotonic(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 3)
	assert.Equal(t, 0, positionOf(t, d, ids[0]))
	assert.Equal(t, 1, positionOf(t, d, ids[1]))
	assert.Equal(t, 2, positionOf(t, d, ids[2]))
}

func TestListCards_projectFilter(t *testing.T) {
	d := testhelpers.TempDB(t)
	seed(t, d, "alpha", "backlog", 2)
	seed(t, d, "beta", "backlog", 3)

	all, err := d.ListCards("")
	require.NoError(t, err)
	assert.Len(t, all, 5)

	alpha, err := d.ListCards("alpha")
	require.NoError(t, err)
	assert.Len(t, alpha, 2)
	for _, c := range alpha {
		assert.Equal(t, "alpha", c.Project)
	}
}

func TestListCards_orderedByColumnThenPosition(t *testing.T) {
	d := testhelpers.TempDB(t)
	bl := seed(t, d, "p", "backlog", 2)
	dn := seed(t, d, "p", "done", 2)
	_ = bl
	_ = dn

	list, err := d.ListCards("p")
	require.NoError(t, err)
	require.Len(t, list, 4)

	// backlog comes before done alphabetically, positions ascending within.
	assert.Equal(t, "backlog", list[0].Column)
	assert.Equal(t, 0, list[0].Position)
	assert.Equal(t, "backlog", list[1].Column)
	assert.Equal(t, 1, list[1].Position)
	assert.Equal(t, "done", list[2].Column)
	assert.Equal(t, 0, list[2].Position)
}

func TestUpdateCard_roundtrip(t *testing.T) {
	d := testhelpers.TempDB(t)
	c := &db.Card{Title: "old", Column: "backlog", Project: "p"}
	require.NoError(t, d.CreateCard(c))

	c.Title = "new"
	c.Priority = "P1"
	require.NoError(t, d.UpdateCard(c))

	got, err := d.GetCard(c.ID)
	require.NoError(t, err)
	assert.Equal(t, "new", got.Title)
	assert.Equal(t, "P1", got.Priority)
}

func TestListCards_excludesArchivedByDefault(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 3)

	// Walk valid transitions: backlog → in_progress → review → done to trigger auto-archive.
	require.NoError(t, d.MoveCard(ids[2], "in_progress", 0))
	require.NoError(t, d.MoveCard(ids[2], "review", 0))
	require.NoError(t, d.MoveCard(ids[2], "done", 0))

	// Default list should exclude the archived card.
	list, err := d.ListCards("p")
	require.NoError(t, err)
	assert.Len(t, list, 2)
	for _, c := range list {
		assert.Equal(t, "backlog", c.Column)
	}

	// With includeArchived=true, all 3 should appear.
	all, err := d.ListCards("p", true)
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestMoveCard_archivesOnDoneUnarchivesOnRestore(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 1)

	// Walk through valid transitions: backlog → in_progress → review → done
	require.NoError(t, d.MoveCard(ids[0], "in_progress", 0))
	require.NoError(t, d.MoveCard(ids[0], "review", 0))
	require.NoError(t, d.MoveCard(ids[0], "done", 0))
	card, err := d.GetCard(ids[0])
	require.NoError(t, err)
	assert.True(t, card.Archived)

	// Move back to backlog — should unarchive.
	require.NoError(t, d.MoveCard(ids[0], "backlog", 0))
	card, err = d.GetCard(ids[0])
	require.NoError(t, err)
	assert.False(t, card.Archived)
}

func TestDeleteCard_cascadesNotes(t *testing.T) {
	d := testhelpers.TempDB(t)
	c := &db.Card{Title: "c", Column: "backlog", Project: "p"}
	require.NoError(t, d.CreateCard(c))

	_, err := d.AddNote(c.ID, "a note")
	require.NoError(t, err)

	notes, err := d.ListNotes(c.ID)
	require.NoError(t, err)
	require.Len(t, notes, 1)

	require.NoError(t, d.DeleteCard(c.ID))

	notes, err = d.ListNotes(c.ID)
	require.NoError(t, err)
	assert.Len(t, notes, 0, "notes should cascade on card delete")
}

// --- MoveCard densify (#442 regression) ---

func TestMoveCard_densifiesDestination(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 5)

	// Move id[4] to position 1
	require.NoError(t, d.MoveCard(ids[4], "backlog", 1))

	// Expected order: [0], [4], [1], [2], [3] at positions 0..4
	assertColumnOrder(t, d, "p", "backlog", []int64{ids[0], ids[4], ids[1], ids[2], ids[3]})
}

func TestMoveCard_dropInPlaceIsNoop(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 4)

	// Drop id[2] at its existing render index (position 2).
	require.NoError(t, d.MoveCard(ids[2], "backlog", 2))
	assertColumnOrder(t, d, "p", "backlog", ids)
}

func TestMoveCard_crossColumnDensifiesBoth(t *testing.T) {
	d := testhelpers.TempDB(t)
	bl := seed(t, d, "p", "backlog", 4)
	ip := seed(t, d, "p", "in_progress", 2)

	// Move bl[1] (pos 1) from backlog to in_progress at pos 0.
	require.NoError(t, d.MoveCard(bl[1], "in_progress", 0))

	// backlog should now be [bl[0], bl[2], bl[3]] at positions 0..2.
	assertColumnOrder(t, d, "p", "backlog", []int64{bl[0], bl[2], bl[3]})
	// in_progress should now be [bl[1], ip[0], ip[1]] at positions 0..2.
	assertColumnOrder(t, d, "p", "in_progress", []int64{bl[1], ip[0], ip[1]})
}

func TestMoveCard_positionClampedToRange(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 3)

	// Out-of-range positive clamps to end.
	require.NoError(t, d.MoveCard(ids[0], "backlog", 99))
	assertColumnOrder(t, d, "p", "backlog", []int64{ids[1], ids[2], ids[0]})

	// Negative clamps to 0.
	require.NoError(t, d.MoveCard(ids[0], "backlog", -5))
	assertColumnOrder(t, d, "p", "backlog", []int64{ids[0], ids[1], ids[2]})
}

func TestMoveCard_childrenSkippedFromDensify(t *testing.T) {
	d := testhelpers.TempDB(t)
	// Parent in backlog.
	parent := &db.Card{Title: "parent", Column: "backlog", Project: "p"}
	require.NoError(t, d.CreateCard(parent))
	// Child with parent_id set — should not participate in column ordering.
	child := &db.Card{Title: "child", Column: "backlog", Project: "p", ParentID: parent.ID}
	require.NoError(t, d.CreateCard(child))
	// Another root sibling.
	sibling := &db.Card{Title: "sib", Column: "backlog", Project: "p"}
	require.NoError(t, d.CreateCard(sibling))

	// Move sibling to position 0. Root order should be [sibling, parent].
	require.NoError(t, d.MoveCard(sibling.ID, "backlog", 0))

	// Only root cards participate in the densified order.
	assertColumnOrder(t, d, "p", "backlog", []int64{sibling.ID, parent.ID})

	// Child's column should still be backlog but its position is unchanged /
	// untracked (not asserted) — the important invariant is that it wasn't
	// re-indexed into the root sequence.
	got, err := d.GetCard(child.ID)
	require.NoError(t, err)
	assert.Equal(t, "backlog", got.Column)
	assert.Equal(t, parent.ID, got.ParentID)
}

// --- Dependencies (#443 regression) ---

func TestAddDependency_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 2)

	require.NoError(t, d.AddDependency(ids[0], ids[1]))

	deps, err := d.ListDependencies("p")
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, ids[0], deps[0].BlockerID)
	assert.Equal(t, ids[1], deps[0].BlockedID)
}

func TestAddDependency_selfLoopRejected(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 1)
	err := d.AddDependency(ids[0], ids[0])
	assert.Error(t, err)
}

func TestAddDependency_directCycleRejected(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 2)

	// A blocks B
	require.NoError(t, d.AddDependency(ids[0], ids[1]))
	// B blocks A should fail.
	err := d.AddDependency(ids[1], ids[0])
	assert.Error(t, err, "direct cycle should be rejected")
}

func TestAddDependency_transitiveCycleRejected(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 3)

	// A → B → C
	require.NoError(t, d.AddDependency(ids[0], ids[1]))
	require.NoError(t, d.AddDependency(ids[1], ids[2]))
	// C → A closes the cycle.
	err := d.AddDependency(ids[2], ids[0])
	assert.Error(t, err, "transitive cycle should be rejected")
}

func TestAddDependency_idempotent(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 2)

	require.NoError(t, d.AddDependency(ids[0], ids[1]))
	require.NoError(t, d.AddDependency(ids[0], ids[1]), "duplicate insert should be silently ignored")

	deps, err := d.ListDependencies("p")
	require.NoError(t, err)
	assert.Len(t, deps, 1)
}

func TestRemoveDependency(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 2)

	require.NoError(t, d.AddDependency(ids[0], ids[1]))
	require.NoError(t, d.RemoveDependency(ids[0], ids[1]))

	deps, err := d.ListDependencies("p")
	require.NoError(t, err)
	assert.Len(t, deps, 0)
}

func TestListDependencies_projectScoped(t *testing.T) {
	d := testhelpers.TempDB(t)
	alphaIDs := seed(t, d, "alpha", "backlog", 2)
	betaIDs := seed(t, d, "beta", "backlog", 2)

	require.NoError(t, d.AddDependency(alphaIDs[0], alphaIDs[1]))
	require.NoError(t, d.AddDependency(betaIDs[0], betaIDs[1]))

	alpha, err := d.ListDependencies("alpha")
	require.NoError(t, err)
	assert.Len(t, alpha, 1)
	assert.Equal(t, alphaIDs[0], alpha[0].BlockerID)

	all, err := d.ListDependencies("")
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestDeleteCard_cascadesDependencies(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 2)
	require.NoError(t, d.AddDependency(ids[0], ids[1]))

	require.NoError(t, d.DeleteCard(ids[0]))

	deps, err := d.ListDependencies("p")
	require.NoError(t, err)
	assert.Len(t, deps, 0, "deps should cascade on card delete")
}

// --- Helper assertions ---

// assertColumnOrder asserts that the root cards in project p / column col
// are exactly the given ids in order, with positions 0..N-1 densified.
func assertColumnOrder(t *testing.T, d *db.DB, p, col string, want []int64) {
	t.Helper()
	list, err := d.ListCards(p, true)
	require.NoError(t, err)

	var got []int64
	for _, c := range list {
		if c.Column != col || c.ParentID != 0 {
			continue
		}
		got = append(got, c.ID)
	}
	require.Equal(t, want, got, "column %q order mismatch", col)

	// Densified: positions should be 0..len-1 in order.
	for i, id := range want {
		pos := positionOf(t, d, id)
		assert.Equalf(t, i, pos, "card %d expected position %d, got %d", id, i, pos)
	}
}

// --- Workflow CRUD + Transition validation ---

func TestWorkflow_defaultFallback(t *testing.T) {
	d := testhelpers.TempDB(t)
	wf, err := d.GetOrDefaultWorkflow("nonexistent")
	require.NoError(t, err)
	assert.Equal(t, db.DefaultWorkflow.Columns, wf.Columns)
	assert.Equal(t, db.DefaultWorkflow.Transitions, wf.Transitions)
}

func TestWorkflow_setAndGet(t *testing.T) {
	d := testhelpers.TempDB(t)
	custom := &db.WorkflowConfig{
		Columns: []db.WorkflowColumn{
			{ID: "todo", Label: "To Do"},
			{ID: "doing", Label: "Doing"},
			{ID: "done", Label: "Done"},
		},
		Transitions: map[string][]string{
			"todo":  {"doing"},
			"doing": {"done", "todo"},
			"done":  {"todo"},
		},
	}
	require.NoError(t, d.SetWorkflow("proj", custom))

	got, err := d.GetWorkflow("proj")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.Columns, 3)
	assert.Equal(t, []string{"doing"}, got.Transitions["todo"])
}

func TestWorkflow_upsertOverwrites(t *testing.T) {
	d := testhelpers.TempDB(t)
	v1 := &db.WorkflowConfig{
		Columns:     []db.WorkflowColumn{{ID: "a", Label: "A"}},
		Transitions: map[string][]string{"a": {}},
	}
	v2 := &db.WorkflowConfig{
		Columns:     []db.WorkflowColumn{{ID: "b", Label: "B"}, {ID: "c", Label: "C"}},
		Transitions: map[string][]string{"b": {"c"}, "c": {"b"}},
	}
	require.NoError(t, d.SetWorkflow("proj", v1))
	require.NoError(t, d.SetWorkflow("proj", v2))

	got, err := d.GetWorkflow("proj")
	require.NoError(t, err)
	assert.Len(t, got.Columns, 2)
}

func TestMoveCard_invalidTransitionBlocked(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 1)

	// backlog → review is not allowed in the default workflow
	err := d.MoveCard(ids[0], "review", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")

	// backlog → done is not allowed directly
	err = d.MoveCard(ids[0], "done", 0)
	assert.Error(t, err)
}

func TestMoveCard_validTransitionAllowed(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "p", "backlog", 1)

	// backlog → in_progress is valid
	require.NoError(t, d.MoveCard(ids[0], "in_progress", 0))

	// in_progress → review is valid
	require.NoError(t, d.MoveCard(ids[0], "review", 0))

	// review → in_progress (send back) is valid
	require.NoError(t, d.MoveCard(ids[0], "in_progress", 0))
}

func TestMoveCard_customWorkflowOverridesDefault(t *testing.T) {
	d := testhelpers.TempDB(t)
	ids := seed(t, d, "custom", "backlog", 1)

	// Set a custom workflow that allows backlog → done directly
	custom := &db.WorkflowConfig{
		Columns: db.DefaultWorkflow.Columns,
		Transitions: map[string][]string{
			"backlog":     {"in_progress", "done"},
			"in_progress": {"review", "backlog"},
			"review":      {"done", "in_progress"},
			"done":        {"backlog"},
		},
	}
	require.NoError(t, d.SetWorkflow("custom", custom))

	// Now backlog → done should work
	require.NoError(t, d.MoveCard(ids[0], "done", 0))
}

func TestSeedDefaultWorkflows_populatesExistingProjects(t *testing.T) {
	d := testhelpers.TempDB(t)

	// Create cards in two projects — seeding runs during Open/migrate,
	// so these projects won't have been seeded yet. Simulate by inserting
	// cards and then calling SetWorkflow only for one of them.
	seed(t, d, "alpha", "backlog", 1)
	seed(t, d, "beta", "backlog", 1)

	// alpha already has a workflow; beta does not.
	// GetOrDefaultWorkflow should return the default for both.
	for _, proj := range []string{"alpha", "beta"} {
		wf, err := d.GetOrDefaultWorkflow(proj)
		require.NoError(t, err)
		assert.Len(t, wf.Columns, 4, "project %s should have 4 default columns", proj)
	}

	// But only seeded projects should have a DB row.
	// Since TempDB runs migrate() which calls seedDefaultWorkflows(),
	// both projects created BEFORE Open() would be seeded.
	// Here the cards are created AFTER Open(), so they won't be seeded
	// automatically — the fallback handles them. This verifies the
	// fallback path works for new projects created after migration.
	wf, err := d.GetWorkflow("beta")
	require.NoError(t, err)
	// No explicit row — fallback handles it.
	assert.Nil(t, wf, "new project shouldn't have an explicit workflow row yet")
}

// --- Refinement gates ---

func TestGate_emptyDescriptionBlocksMove(t *testing.T) {
	d := testhelpers.TempDB(t)
	c := &db.Card{Title: "no desc", Column: "backlog", Project: "p"}
	require.NoError(t, d.CreateCard(c))

	err := d.MoveCard(c.ID, "in_progress", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires a description")
}

func TestGate_withDescriptionAllowsMove(t *testing.T) {
	d := testhelpers.TempDB(t)
	c := &db.Card{Title: "has desc", Column: "backlog", Project: "p", Description: "implement the thing"}
	require.NoError(t, d.CreateCard(c))

	require.NoError(t, d.MoveCard(c.ID, "in_progress", 0))
}

func TestGate_checklistRequired(t *testing.T) {
	d := testhelpers.TempDB(t)

	// Set a custom workflow with a checklist gate
	wf := &db.WorkflowConfig{
		Columns: db.DefaultWorkflow.Columns,
		Transitions: db.DefaultWorkflow.Transitions,
		Gates: map[string]db.TransitionGate{
			"backlog→in_progress": {RequireDescription: true, RequireChecklist: true},
		},
	}
	require.NoError(t, d.SetWorkflow("gated", wf))

	// Card with description but no checklist — should fail
	c := &db.Card{Title: "no checklist", Column: "backlog", Project: "gated", Description: "just text"}
	require.NoError(t, d.CreateCard(c))
	err := d.MoveCard(c.ID, "in_progress", 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "checklist")

	// Add a checklist item — should pass
	c.Description = "plan:\n- [ ] step one\n- [ ] step two"
	require.NoError(t, d.UpdateCard(c))
	require.NoError(t, d.MoveCard(c.ID, "in_progress", 0))
}

func TestGate_completedChecklistPasses(t *testing.T) {
	d := testhelpers.TempDB(t)

	wf := &db.WorkflowConfig{
		Columns: db.DefaultWorkflow.Columns,
		Transitions: db.DefaultWorkflow.Transitions,
		Gates: map[string]db.TransitionGate{
			"backlog→in_progress": {RequireChecklist: true},
		},
	}
	require.NoError(t, d.SetWorkflow("gated2", wf))

	// Card with completed checkboxes — should pass (has checklist items)
	c := &db.Card{Title: "done checklist", Column: "backlog", Project: "gated2", Description: "- [x] done"}
	require.NoError(t, d.CreateCard(c))
	require.NoError(t, d.MoveCard(c.ID, "in_progress", 0))
}

func TestGate_noGateOnOtherTransitions(t *testing.T) {
	d := testhelpers.TempDB(t)
	// Card with description can move backlog → in_progress → review without gate issues
	c := &db.Card{Title: "full flow", Column: "backlog", Project: "p", Description: "has desc"}
	require.NoError(t, d.CreateCard(c))
	require.NoError(t, d.MoveCard(c.ID, "in_progress", 0))
	// in_progress → review has no gate in default workflow
	require.NoError(t, d.MoveCard(c.ID, "review", 0))
}
