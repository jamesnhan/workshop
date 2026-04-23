package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetWorkflow_nonExistent(t *testing.T) {
	d := testhelpers.TempDB(t)

	wf, err := d.GetWorkflow("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, wf, "should return nil for non-existent project")
}

func TestSetWorkflow_andGet(t *testing.T) {
	d := testhelpers.TempDB(t)

	wf := &db.WorkflowConfig{
		Columns: []db.WorkflowColumn{
			{ID: "todo", Label: "To Do"},
			{ID: "done", Label: "Done"},
		},
		Transitions: map[string][]string{
			"todo": {"done"},
			"done": {"todo"},
		},
	}
	require.NoError(t, d.SetWorkflow("myproject", wf))

	got, err := d.GetWorkflow("myproject")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.Columns, 2)
	assert.Equal(t, "todo", got.Columns[0].ID)
	assert.Equal(t, []string{"done"}, got.Transitions["todo"])
}

func TestSetWorkflow_withGates(t *testing.T) {
	d := testhelpers.TempDB(t)

	wf := &db.WorkflowConfig{
		Columns: db.DefaultWorkflow.Columns,
		Transitions: db.DefaultWorkflow.Transitions,
		Gates: map[string]db.TransitionGate{
			"backlog→in_progress": {RequireDescription: true, RequireChecklist: true},
			"review→done":        {RequireDescription: true},
		},
	}
	require.NoError(t, d.SetWorkflow("gated", wf))

	got, err := d.GetWorkflow("gated")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Gates)
	assert.True(t, got.Gates["backlog→in_progress"].RequireDescription)
	assert.True(t, got.Gates["backlog→in_progress"].RequireChecklist)
	assert.True(t, got.Gates["review→done"].RequireDescription)
	assert.False(t, got.Gates["review→done"].RequireChecklist)
}

func TestSetWorkflow_upsert(t *testing.T) {
	d := testhelpers.TempDB(t)

	v1 := &db.WorkflowConfig{
		Columns: []db.WorkflowColumn{{ID: "a", Label: "A"}},
		Transitions: map[string][]string{"a": {}},
	}
	require.NoError(t, d.SetWorkflow("proj", v1))

	v2 := &db.WorkflowConfig{
		Columns: []db.WorkflowColumn{{ID: "x", Label: "X"}, {ID: "y", Label: "Y"}},
		Transitions: map[string][]string{"x": {"y"}, "y": {"x"}},
	}
	require.NoError(t, d.SetWorkflow("proj", v2))

	got, err := d.GetWorkflow("proj")
	require.NoError(t, err)
	assert.Len(t, got.Columns, 2)
	assert.Equal(t, "x", got.Columns[0].ID)
}

func TestGetOrDefaultWorkflow_returnsDefaultWhenNone(t *testing.T) {
	d := testhelpers.TempDB(t)

	wf, err := d.GetOrDefaultWorkflow("newproject")
	require.NoError(t, err)
	require.NotNil(t, wf)
	assert.Equal(t, db.DefaultWorkflow.Columns, wf.Columns)
	assert.Equal(t, db.DefaultWorkflow.Transitions, wf.Transitions)
}

func TestGetOrDefaultWorkflow_returnsCustomWhenSet(t *testing.T) {
	d := testhelpers.TempDB(t)

	custom := &db.WorkflowConfig{
		Columns: []db.WorkflowColumn{{ID: "alpha", Label: "Alpha"}},
		Transitions: map[string][]string{"alpha": {}},
	}
	require.NoError(t, d.SetWorkflow("proj", custom))

	wf, err := d.GetOrDefaultWorkflow("proj")
	require.NoError(t, err)
	require.NotNil(t, wf)
	assert.Len(t, wf.Columns, 1)
	assert.Equal(t, "alpha", wf.Columns[0].ID)
}

func TestValidateTransition_sameColumnAlwaysAllowed(t *testing.T) {
	d := testhelpers.TempDB(t)

	err := d.ValidateTransition("anyproject", "backlog", "backlog")
	require.NoError(t, err, "reorder within same column should always be allowed")
}

func TestValidateTransition_allowedTransition(t *testing.T) {
	d := testhelpers.TempDB(t)

	err := d.ValidateTransition("anyproject", "backlog", "in_progress")
	require.NoError(t, err)
}

func TestValidateTransition_blockedTransition(t *testing.T) {
	d := testhelpers.TempDB(t)

	err := d.ValidateTransition("anyproject", "backlog", "done")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func TestValidateTransition_unknownSourceColumn(t *testing.T) {
	d := testhelpers.TempDB(t)

	err := d.ValidateTransition("anyproject", "nonexistent_col", "backlog")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown source column")
}

func TestValidateGates_noGateAllowsMove(t *testing.T) {
	d := testhelpers.TempDB(t)

	card := &db.Card{Description: ""}
	err := d.ValidateGates("anyproject", card, "in_progress", "review")
	require.NoError(t, err, "no gate on this transition should allow move")
}

func TestValidateGates_sameColumnAlwaysAllowed(t *testing.T) {
	d := testhelpers.TempDB(t)

	card := &db.Card{Description: ""}
	err := d.ValidateGates("anyproject", card, "backlog", "backlog")
	require.NoError(t, err)
}

func TestValidateGates_requireDescriptionBlocks(t *testing.T) {
	d := testhelpers.TempDB(t)

	card := &db.Card{Description: ""}
	err := d.ValidateGates("anyproject", card, "backlog", "in_progress")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires a description")
}

func TestValidateGates_requireDescriptionPasses(t *testing.T) {
	d := testhelpers.TempDB(t)

	card := &db.Card{Description: "has content"}
	err := d.ValidateGates("anyproject", card, "backlog", "in_progress")
	require.NoError(t, err)
}

func TestValidateGates_nilGatesMap(t *testing.T) {
	d := testhelpers.TempDB(t)

	// Set a workflow with no gates
	wf := &db.WorkflowConfig{
		Columns: db.DefaultWorkflow.Columns,
		Transitions: db.DefaultWorkflow.Transitions,
		// Gates is nil
	}
	require.NoError(t, d.SetWorkflow("nogates", wf))

	card := &db.Card{Description: ""}
	err := d.ValidateGates("nogates", card, "backlog", "in_progress")
	require.NoError(t, err, "nil gates map should allow all transitions")
}
