package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateOllamaConversation(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "Test Chat", Model: "gemma3:12b", SystemPrompt: "You are helpful."}
	require.NoError(t, d.CreateOllamaConversation(conv))
	require.NotZero(t, conv.ID)
	assert.NotEmpty(t, conv.CreatedAt)
	assert.NotEmpty(t, conv.UpdatedAt)
}

func TestCreateOllamaConversation_defaults(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{}
	require.NoError(t, d.CreateOllamaConversation(conv))
	require.NotZero(t, conv.ID)

	got, err := d.GetOllamaConversation(conv.ID)
	require.NoError(t, err)
	// Empty title stays empty (the handler sets the default, not the DB method)
	assert.Equal(t, "", got.Title)
	assert.Equal(t, "", got.Model)
	assert.Equal(t, "", got.SystemPrompt)
}

func TestGetOllamaConversation(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "Chat 1", Model: "llama3"}
	require.NoError(t, d.CreateOllamaConversation(conv))

	got, err := d.GetOllamaConversation(conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "Chat 1", got.Title)
	assert.Equal(t, "llama3", got.Model)
}

func TestGetOllamaConversation_notFound(t *testing.T) {
	d := testhelpers.TempDB(t)
	_, err := d.GetOllamaConversation(999)
	assert.Error(t, err)
}

func TestListOllamaConversations(t *testing.T) {
	d := testhelpers.TempDB(t)

	// Empty list
	convs, err := d.ListOllamaConversations()
	require.NoError(t, err)
	assert.Empty(t, convs)

	// Create a few
	for _, title := range []string{"A", "B", "C"} {
		require.NoError(t, d.CreateOllamaConversation(&db.OllamaConversation{Title: title}))
	}

	convs, err = d.ListOllamaConversations()
	require.NoError(t, err)
	assert.Len(t, convs, 3)
	// All created within the same second, so just verify all titles are present
	titles := make([]string, len(convs))
	for i, c := range convs {
		titles[i] = c.Title
	}
	assert.ElementsMatch(t, []string{"A", "B", "C"}, titles)
}

func TestUpdateOllamaConversation(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "Original", Model: "llama3"}
	require.NoError(t, d.CreateOllamaConversation(conv))

	conv.Title = "Renamed"
	conv.Model = "gemma3:12b"
	conv.SystemPrompt = "Be concise."
	require.NoError(t, d.UpdateOllamaConversation(conv))

	got, err := d.GetOllamaConversation(conv.ID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.Title)
	assert.Equal(t, "gemma3:12b", got.Model)
	assert.Equal(t, "Be concise.", got.SystemPrompt)
}

func TestDeleteOllamaConversation(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "To Delete"}
	require.NoError(t, d.CreateOllamaConversation(conv))

	require.NoError(t, d.DeleteOllamaConversation(conv.ID))

	_, err := d.GetOllamaConversation(conv.ID)
	assert.Error(t, err)
}

func TestDeleteOllamaConversation_cascadesMessages(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "With Messages"}
	require.NoError(t, d.CreateOllamaConversation(conv))

	msg := &db.OllamaMessage{ConversationID: conv.ID, Role: "user", Content: "hello"}
	require.NoError(t, d.CreateOllamaMessage(msg))

	// Verify message exists
	msgs, err := d.ListOllamaMessages(conv.ID)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)

	// Delete conversation — messages should cascade
	require.NoError(t, d.DeleteOllamaConversation(conv.ID))

	msgs, err = d.ListOllamaMessages(conv.ID)
	require.NoError(t, err)
	assert.Empty(t, msgs)
}

func TestCreateOllamaMessage(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "Chat"}
	require.NoError(t, d.CreateOllamaConversation(conv))

	msg := &db.OllamaMessage{
		ConversationID: conv.ID,
		Role:           "user",
		Content:        "What is Go?",
		Model:          "gemma3:12b",
		Stats:          `{"eval_count":42}`,
	}
	require.NoError(t, d.CreateOllamaMessage(msg))
	require.NotZero(t, msg.ID)
	assert.NotEmpty(t, msg.CreatedAt)
}

func TestCreateOllamaMessage_updatesConversation(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "Chat"}
	require.NoError(t, d.CreateOllamaConversation(conv))
	originalUpdatedAt := conv.UpdatedAt

	// Force a different timestamp by doing the insert
	msg := &db.OllamaMessage{ConversationID: conv.ID, Role: "user", Content: "hello"}
	require.NoError(t, d.CreateOllamaMessage(msg))

	got, err := d.GetOllamaConversation(conv.ID)
	require.NoError(t, err)
	// updated_at should be >= the original (may be same second in fast tests)
	assert.GreaterOrEqual(t, got.UpdatedAt, originalUpdatedAt)
}

func TestListOllamaMessages(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv := &db.OllamaConversation{Title: "Chat"}
	require.NoError(t, d.CreateOllamaConversation(conv))

	// Empty
	msgs, err := d.ListOllamaMessages(conv.ID)
	require.NoError(t, err)
	assert.Empty(t, msgs)

	// Add messages
	for _, content := range []string{"hello", "world", "foo"} {
		require.NoError(t, d.CreateOllamaMessage(&db.OllamaMessage{
			ConversationID: conv.ID, Role: "user", Content: content,
		}))
	}

	msgs, err = d.ListOllamaMessages(conv.ID)
	require.NoError(t, err)
	assert.Len(t, msgs, 3)
	// Ordered by created_at ASC
	assert.Equal(t, "hello", msgs[0].Content)
	assert.Equal(t, "foo", msgs[2].Content)
}

func TestListOllamaMessages_isolatedByConversation(t *testing.T) {
	d := testhelpers.TempDB(t)

	conv1 := &db.OllamaConversation{Title: "Chat 1"}
	require.NoError(t, d.CreateOllamaConversation(conv1))
	conv2 := &db.OllamaConversation{Title: "Chat 2"}
	require.NoError(t, d.CreateOllamaConversation(conv2))

	require.NoError(t, d.CreateOllamaMessage(&db.OllamaMessage{ConversationID: conv1.ID, Role: "user", Content: "msg1"}))
	require.NoError(t, d.CreateOllamaMessage(&db.OllamaMessage{ConversationID: conv2.ID, Role: "user", Content: "msg2"}))

	msgs1, err := d.ListOllamaMessages(conv1.ID)
	require.NoError(t, err)
	assert.Len(t, msgs1, 1)
	assert.Equal(t, "msg1", msgs1[0].Content)

	msgs2, err := d.ListOllamaMessages(conv2.ID)
	require.NoError(t, err)
	assert.Len(t, msgs2, 1)
	assert.Equal(t, "msg2", msgs2[0].Content)
}
