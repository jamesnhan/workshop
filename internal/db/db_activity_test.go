package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordActivity_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)

	entry := &db.ActivityEntry{
		PaneTarget: "agent:0.0",
		AgentName:  "orchestrator",
		ActionType: "file_write",
		Summary:    "Created main.go",
		Metadata:   `{"file":"main.go"}`,
		Project:    "workshop",
	}
	id, err := d.RecordActivity(entry)
	require.NoError(t, err)
	assert.NotZero(t, id)
}

func TestRecordActivity_defaultMetadata(t *testing.T) {
	d := testhelpers.TempDB(t)

	entry := &db.ActivityEntry{
		ActionType: "status",
		Summary:    "Started",
	}
	id, err := d.RecordActivity(entry)
	require.NoError(t, err)
	assert.NotZero(t, id)

	// Verify the metadata was set to "{}" by default
	entries, err := d.ListActivity("", "", "", 10, false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "{}", entries[0].Metadata)
}

func TestRecordActivity_withParentID(t *testing.T) {
	d := testhelpers.TempDB(t)

	parent := &db.ActivityEntry{
		ActionType: "phase_start",
		Summary:    "Phase: Plan",
		Project:    "workshop",
	}
	parentID, err := d.RecordActivity(parent)
	require.NoError(t, err)

	child := &db.ActivityEntry{
		ParentID:   parentID,
		ActionType: "file_write",
		Summary:    "Created plan.md",
		Project:    "workshop",
	}
	childID, err := d.RecordActivity(child)
	require.NoError(t, err)
	assert.NotEqual(t, parentID, childID)
}

func TestListActivity_noFilters(t *testing.T) {
	d := testhelpers.TempDB(t)

	for i := 0; i < 5; i++ {
		_, err := d.RecordActivity(&db.ActivityEntry{
			ActionType: "status",
			Summary:    "entry",
			Project:    "workshop",
		})
		require.NoError(t, err)
	}

	entries, err := d.ListActivity("", "", "", 100, false)
	require.NoError(t, err)
	assert.Len(t, entries, 5)
}

func TestListActivity_paneFilter(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.RecordActivity(&db.ActivityEntry{PaneTarget: "agent:0.0", ActionType: "status", Summary: "a"})
	require.NoError(t, err)
	_, err = d.RecordActivity(&db.ActivityEntry{PaneTarget: "agent:0.1", ActionType: "status", Summary: "b"})
	require.NoError(t, err)

	entries, err := d.ListActivity("agent:0.0", "", "", 100, false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "a", entries[0].Summary)
}

func TestListActivity_projectFilter(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.RecordActivity(&db.ActivityEntry{ActionType: "status", Summary: "a", Project: "workshop"})
	require.NoError(t, err)
	_, err = d.RecordActivity(&db.ActivityEntry{ActionType: "status", Summary: "b", Project: "roblox"})
	require.NoError(t, err)

	entries, err := d.ListActivity("", "workshop", "", 100, false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "a", entries[0].Summary)
}

func TestListActivity_actionTypeFilter(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.RecordActivity(&db.ActivityEntry{ActionType: "file_write", Summary: "a"})
	require.NoError(t, err)
	_, err = d.RecordActivity(&db.ActivityEntry{ActionType: "command", Summary: "b"})
	require.NoError(t, err)

	entries, err := d.ListActivity("", "", "file_write", 100, false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "a", entries[0].Summary)
}

func TestListActivity_limit(t *testing.T) {
	d := testhelpers.TempDB(t)

	for i := 0; i < 10; i++ {
		_, err := d.RecordActivity(&db.ActivityEntry{ActionType: "status", Summary: "x"})
		require.NoError(t, err)
	}

	entries, err := d.ListActivity("", "", "", 3, false)
	require.NoError(t, err)
	assert.Len(t, entries, 3)
}

func TestListActivity_defaultLimit(t *testing.T) {
	d := testhelpers.TempDB(t)

	// With limit <= 0, should default to 100
	entries, err := d.ListActivity("", "", "", 0, false)
	require.NoError(t, err)
	assert.Empty(t, entries) // no data, just testing it doesn't error
}

func TestListActivity_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	entries, err := d.ListActivity("", "", "", 100, false)
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestListActivity_treeMode(t *testing.T) {
	d := testhelpers.TempDB(t)

	parentID, err := d.RecordActivity(&db.ActivityEntry{
		ActionType: "phase_start",
		Summary:    "Phase: Plan",
		Project:    "workshop",
	})
	require.NoError(t, err)

	_, err = d.RecordActivity(&db.ActivityEntry{
		ParentID:   parentID,
		ActionType: "file_write",
		Summary:    "Created plan.md",
		Project:    "workshop",
	})
	require.NoError(t, err)

	_, err = d.RecordActivity(&db.ActivityEntry{
		ParentID:   parentID,
		ActionType: "command",
		Summary:    "Ran tests",
		Project:    "workshop",
	})
	require.NoError(t, err)

	// Flat mode: all 3 entries
	flat, err := d.ListActivity("", "", "", 100, false)
	require.NoError(t, err)
	assert.Len(t, flat, 3)

	// Tree mode: 1 root with 2 children
	tree, err := d.ListActivity("", "", "", 100, true)
	require.NoError(t, err)
	require.Len(t, tree, 1)
	assert.Equal(t, "Phase: Plan", tree[0].Summary)
	assert.Len(t, tree[0].Children, 2)
}

func TestListActivity_treeMode_orphanedChildrenAreRoots(t *testing.T) {
	d := testhelpers.TempDB(t)

	// Create a child with a parent_id that doesn't exist in the result set
	_, err := d.RecordActivity(&db.ActivityEntry{
		ParentID:   9999,
		ActionType: "status",
		Summary:    "orphan",
	})
	require.NoError(t, err)

	tree, err := d.ListActivity("", "", "", 100, true)
	require.NoError(t, err)
	// Orphaned child should appear as a root entry
	require.Len(t, tree, 1)
	assert.Equal(t, "orphan", tree[0].Summary)
}

func TestListActivity_combinedFilters(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.RecordActivity(&db.ActivityEntry{
		PaneTarget: "agent:0.0",
		ActionType: "file_write",
		Summary:    "match",
		Project:    "workshop",
	})
	require.NoError(t, err)
	_, err = d.RecordActivity(&db.ActivityEntry{
		PaneTarget: "agent:0.1",
		ActionType: "file_write",
		Summary:    "wrong pane",
		Project:    "workshop",
	})
	require.NoError(t, err)
	_, err = d.RecordActivity(&db.ActivityEntry{
		PaneTarget: "agent:0.0",
		ActionType: "command",
		Summary:    "wrong action",
		Project:    "workshop",
	})
	require.NoError(t, err)

	entries, err := d.ListActivity("agent:0.0", "workshop", "file_write", 100, false)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "match", entries[0].Summary)
}
