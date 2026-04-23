package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Card Activity Log ---

func TestLogCardEvent_andListCardLog(t *testing.T) {
	d := testhelpers.TempDB(t)

	c := &db.Card{Title: "test", Column: "backlog", Project: "p", Description: "desc"}
	require.NoError(t, d.CreateCard(c))

	d.LogCardEvent(c.ID, "custom_action", "before", "after", "agent")

	entries, err := d.ListCardLog(c.ID)
	require.NoError(t, err)
	// At least the "created" entry + our custom_action
	require.GreaterOrEqual(t, len(entries), 2)

	// Find our custom action
	var found bool
	for _, e := range entries {
		if e.Action == "custom_action" {
			found = true
			assert.Equal(t, "before", e.BeforeValue)
			assert.Equal(t, "after", e.AfterValue)
			assert.Equal(t, "agent", e.Source)
			assert.NotEmpty(t, e.CreatedAt)
		}
	}
	assert.True(t, found, "custom_action should appear in card log")
}

func TestLogCardEvent_defaultSource(t *testing.T) {
	d := testhelpers.TempDB(t)

	c := &db.Card{Title: "test", Column: "backlog", Project: "p", Description: "desc"}
	require.NoError(t, d.CreateCard(c))

	// Empty source should default to "user"
	d.LogCardEvent(c.ID, "test_action", "", "", "")

	entries, err := d.ListCardLog(c.ID)
	require.NoError(t, err)

	var found bool
	for _, e := range entries {
		if e.Action == "test_action" {
			found = true
			assert.Equal(t, "user", e.Source)
		}
	}
	assert.True(t, found)
}

func TestListCardLog_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	entries, err := d.ListCardLog(9999)
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestListCardLog_containsAllEvents(t *testing.T) {
	d := testhelpers.TempDB(t)

	c := &db.Card{Title: "test", Column: "backlog", Project: "p", Description: "desc"}
	require.NoError(t, d.CreateCard(c))

	d.LogCardEvent(c.ID, "first", "", "", "user")
	d.LogCardEvent(c.ID, "second", "", "", "user")
	d.LogCardEvent(c.ID, "third", "", "", "user")

	entries, err := d.ListCardLog(c.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(entries), 4) // "created" + 3 custom

	// Collect all action names
	actions := make(map[string]bool)
	for _, e := range entries {
		actions[e.Action] = true
	}
	assert.True(t, actions["created"], "should have 'created' event")
	assert.True(t, actions["first"], "should have 'first' event")
	assert.True(t, actions["second"], "should have 'second' event")
	assert.True(t, actions["third"], "should have 'third' event")
}

func TestListProjectLog_allProjects(t *testing.T) {
	d := testhelpers.TempDB(t)

	c1 := &db.Card{Title: "c1", Column: "backlog", Project: "alpha", Description: "desc"}
	require.NoError(t, d.CreateCard(c1))
	c2 := &db.Card{Title: "c2", Column: "backlog", Project: "beta", Description: "desc"}
	require.NoError(t, d.CreateCard(c2))

	entries, err := d.ListProjectLog("", 100)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 2) // at least "created" for each card
}

func TestListProjectLog_projectFilter(t *testing.T) {
	d := testhelpers.TempDB(t)

	c1 := &db.Card{Title: "c1", Column: "backlog", Project: "alpha", Description: "desc"}
	require.NoError(t, d.CreateCard(c1))
	c2 := &db.Card{Title: "c2", Column: "backlog", Project: "beta", Description: "desc"}
	require.NoError(t, d.CreateCard(c2))

	entries, err := d.ListProjectLog("alpha", 100)
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, c1.ID, e.CardID)
	}
}

func TestListProjectLog_defaultLimit(t *testing.T) {
	d := testhelpers.TempDB(t)

	// limit <= 0 defaults to 100
	entries, err := d.ListProjectLog("", 0)
	require.NoError(t, err)
	assert.Nil(t, entries) // no data
}

// --- Card Messages ---

func TestAddMessage_andList(t *testing.T) {
	d := testhelpers.TempDB(t)

	c := &db.Card{Title: "test", Column: "backlog", Project: "p", Description: "desc"}
	require.NoError(t, d.CreateCard(c))

	msg, err := d.AddMessage(c.ID, "agent-1", "hello world")
	require.NoError(t, err)
	require.NotZero(t, msg.ID)
	assert.Equal(t, c.ID, msg.CardID)
	assert.Equal(t, "agent-1", msg.Author)
	assert.Equal(t, "hello world", msg.Text)

	msgs, err := d.ListMessages(c.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "hello world", msgs[0].Text)
	assert.Equal(t, "agent-1", msgs[0].Author)
}

func TestListMessages_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	c := &db.Card{Title: "test", Column: "backlog", Project: "p", Description: "desc"}
	require.NoError(t, d.CreateCard(c))

	msgs, err := d.ListMessages(c.ID)
	require.NoError(t, err)
	assert.Nil(t, msgs)
}

func TestListMessages_orderedByCreatedAt(t *testing.T) {
	d := testhelpers.TempDB(t)

	c := &db.Card{Title: "test", Column: "backlog", Project: "p", Description: "desc"}
	require.NoError(t, d.CreateCard(c))

	_, err := d.AddMessage(c.ID, "a", "first")
	require.NoError(t, err)
	_, err = d.AddMessage(c.ID, "b", "second")
	require.NoError(t, err)
	_, err = d.AddMessage(c.ID, "c", "third")
	require.NoError(t, err)

	msgs, err := d.ListMessages(c.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	assert.Equal(t, "first", msgs[0].Text)
	assert.Equal(t, "second", msgs[1].Text)
	assert.Equal(t, "third", msgs[2].Text)
}

// --- ListProjects ---

func TestListProjects_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)

	seed(t, d, "alpha", "backlog", 1)
	seed(t, d, "beta", "backlog", 1)
	seed(t, d, "gamma", "backlog", 1)

	projects, err := d.ListProjects()
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, projects)
}

func TestListProjects_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	projects, err := d.ListProjects()
	require.NoError(t, err)
	assert.Nil(t, projects)
}

func TestListProjects_excludesEmptyProject(t *testing.T) {
	d := testhelpers.TempDB(t)

	// Card with empty project string
	c := &db.Card{Title: "orphan", Column: "backlog", Project: "", Description: "desc"}
	require.NoError(t, d.CreateCard(c))
	seed(t, d, "real", "backlog", 1)

	projects, err := d.ListProjects()
	require.NoError(t, err)
	assert.Equal(t, []string{"real"}, projects)
}

func TestListProjects_deduplicates(t *testing.T) {
	d := testhelpers.TempDB(t)

	seed(t, d, "same", "backlog", 5)

	projects, err := d.ListProjects()
	require.NoError(t, err)
	assert.Equal(t, []string{"same"}, projects)
}

// --- Recordings ---

func TestCreateRecording_andGet(t *testing.T) {
	d := testhelpers.TempDB(t)

	id, err := d.CreateRecording("test-rec", "main:0.0", 120, 40)
	require.NoError(t, err)
	require.NotZero(t, id)

	rec, err := d.GetRecording(id)
	require.NoError(t, err)
	assert.Equal(t, "test-rec", rec.Name)
	assert.Equal(t, "main:0.0", rec.Target)
	assert.Equal(t, 120, rec.Cols)
	assert.Equal(t, 40, rec.Rows)
	assert.Equal(t, "recording", rec.Status)
	assert.Equal(t, int64(0), rec.DurationMs)
}

func TestGetRecording_notFound(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.GetRecording(9999)
	assert.Error(t, err)
}

func TestStopRecording(t *testing.T) {
	d := testhelpers.TempDB(t)

	id, err := d.CreateRecording("rec", "t:0.0", 80, 24)
	require.NoError(t, err)

	require.NoError(t, d.StopRecording(id, 5000))

	rec, err := d.GetRecording(id)
	require.NoError(t, err)
	assert.Equal(t, "stopped", rec.Status)
	assert.Equal(t, int64(5000), rec.DurationMs)
}

func TestListRecordings(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.CreateRecording("rec1", "t:0.0", 80, 24)
	require.NoError(t, err)
	_, err = d.CreateRecording("rec2", "t:0.1", 120, 40)
	require.NoError(t, err)

	recs, err := d.ListRecordings()
	require.NoError(t, err)
	require.Len(t, recs, 2)
	// Ordered by id DESC
	assert.Equal(t, "rec2", recs[0].Name)
	assert.Equal(t, "rec1", recs[1].Name)
}

func TestListRecordings_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	recs, err := d.ListRecordings()
	require.NoError(t, err)
	assert.Nil(t, recs)
}

func TestAppendFrame_andGetFrames(t *testing.T) {
	d := testhelpers.TempDB(t)

	id, err := d.CreateRecording("rec", "t:0.0", 80, 24)
	require.NoError(t, err)

	require.NoError(t, d.AppendFrame(id, 0, []byte("frame0")))
	require.NoError(t, d.AppendFrame(id, 100, []byte("frame1")))
	require.NoError(t, d.AppendFrame(id, 200, []byte("frame2")))

	frames, err := d.GetRecordingFrames(id)
	require.NoError(t, err)
	require.Len(t, frames, 3)
	assert.Equal(t, 0, frames[0].OffsetMs)
	assert.Equal(t, "frame0", frames[0].Data)
	assert.Equal(t, 100, frames[1].OffsetMs)
	assert.Equal(t, 200, frames[2].OffsetMs)
}

func TestDeleteRecording_cascadesFrames(t *testing.T) {
	d := testhelpers.TempDB(t)

	id, err := d.CreateRecording("rec", "t:0.0", 80, 24)
	require.NoError(t, err)

	require.NoError(t, d.AppendFrame(id, 0, []byte("data")))

	require.NoError(t, d.DeleteRecording(id))

	_, err = d.GetRecording(id)
	assert.Error(t, err)

	frames, err := d.GetRecordingFrames(id)
	require.NoError(t, err)
	assert.Nil(t, frames)
}

// --- Agent Presets ---

func TestListPresets_seededDefaults(t *testing.T) {
	d := testhelpers.TempDB(t)

	presets, err := d.ListPresets()
	require.NoError(t, err)
	// TempDB runs migrate which seeds defaults
	assert.GreaterOrEqual(t, len(presets), 1, "should have at least seeded presets")
}

func TestUpsertPreset_createAndUpdate(t *testing.T) {
	d := testhelpers.TempDB(t)

	p := &db.AgentPreset{
		Name:         "custom",
		Description:  "Custom agent",
		Provider:     "claude",
		Model:        "sonnet",
		Prompt:       "Do the thing",
		SystemPrompt: "You are custom",
		Directory:    "/home/user",
	}
	require.NoError(t, d.UpsertPreset(p))

	got, err := d.GetPreset("custom")
	require.NoError(t, err)
	assert.Equal(t, "Custom agent", got.Description)
	assert.Equal(t, "sonnet", got.Model)

	// Update
	p.Description = "Updated custom agent"
	p.Model = "opus"
	require.NoError(t, d.UpsertPreset(p))

	got, err = d.GetPreset("custom")
	require.NoError(t, err)
	assert.Equal(t, "Updated custom agent", got.Description)
	assert.Equal(t, "opus", got.Model)
}

func TestGetPreset_notFound(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.GetPreset("nonexistent")
	assert.Error(t, err)
}

func TestDeletePreset(t *testing.T) {
	d := testhelpers.TempDB(t)

	p := &db.AgentPreset{Name: "to-delete", Description: "temp"}
	require.NoError(t, d.UpsertPreset(p))

	require.NoError(t, d.DeletePreset("to-delete"))

	_, err := d.GetPreset("to-delete")
	assert.Error(t, err)
}
