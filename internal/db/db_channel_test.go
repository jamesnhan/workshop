package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Channel Subscriptions ---

func TestCreateChannelSubscription_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)

	err := d.CreateChannelSubscription("updates", "agent:0.0", "workshop")
	require.NoError(t, err)

	subs, err := d.ListChannelSubscribers("updates")
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "agent:0.0", subs[0])
}

func TestCreateChannelSubscription_idempotent(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.0", "workshop"))
	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.0", "workshop"))

	subs, err := d.ListChannelSubscribers("updates")
	require.NoError(t, err)
	assert.Len(t, subs, 1, "duplicate subscription should be ignored")
}

func TestCreateChannelSubscription_multipleTargets(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.0", "workshop"))
	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.1", "workshop"))
	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.2", "workshop"))

	subs, err := d.ListChannelSubscribers("updates")
	require.NoError(t, err)
	assert.Len(t, subs, 3)
}

func TestDeleteChannelSubscription_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.0", "workshop"))
	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.1", "workshop"))

	require.NoError(t, d.DeleteChannelSubscription("updates", "agent:0.0"))

	subs, err := d.ListChannelSubscribers("updates")
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "agent:0.1", subs[0])
}

func TestDeleteChannelSubscription_nonExistent(t *testing.T) {
	d := testhelpers.TempDB(t)

	// Deleting a non-existent subscription should not error
	err := d.DeleteChannelSubscription("updates", "agent:0.0")
	require.NoError(t, err)
}

func TestListChannelSubscribers_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	subs, err := d.ListChannelSubscribers("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, subs)
}

func TestListChannelSubscribers_isolatedByChannel(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("alpha", "agent:0.0", ""))
	require.NoError(t, d.CreateChannelSubscription("beta", "agent:0.1", ""))

	alphaSubs, err := d.ListChannelSubscribers("alpha")
	require.NoError(t, err)
	require.Len(t, alphaSubs, 1)
	assert.Equal(t, "agent:0.0", alphaSubs[0])

	betaSubs, err := d.ListChannelSubscribers("beta")
	require.NoError(t, err)
	require.Len(t, betaSubs, 1)
	assert.Equal(t, "agent:0.1", betaSubs[0])
}

// --- Channel Messages ---

func TestCreateChannelMessage_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)

	msg, err := d.CreateChannelMessage("updates", "agent:0.0", "hello world", "workshop")
	require.NoError(t, err)
	require.NotZero(t, msg.ID)
	assert.Equal(t, "updates", msg.Channel)
	assert.Equal(t, "agent:0.0", msg.Sender)
	assert.Equal(t, "hello world", msg.Body)
	assert.Equal(t, "workshop", msg.Project)
	assert.False(t, msg.CreatedAt.IsZero())
}

func TestListChannelMessages_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.CreateChannelMessage("updates", "agent:0.0", "msg1", "workshop")
	require.NoError(t, err)
	_, err = d.CreateChannelMessage("updates", "agent:0.1", "msg2", "workshop")
	require.NoError(t, err)
	_, err = d.CreateChannelMessage("updates", "agent:0.0", "msg3", "workshop")
	require.NoError(t, err)

	msgs, err := d.ListChannelMessages("updates", 10)
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	// Verify all messages are present (order within same second is non-deterministic)
	bodies := make([]string, len(msgs))
	for i, m := range msgs {
		bodies[i] = m.Body
	}
	assert.ElementsMatch(t, []string{"msg1", "msg2", "msg3"}, bodies)
}

func TestListChannelMessages_limit(t *testing.T) {
	d := testhelpers.TempDB(t)

	for i := 0; i < 5; i++ {
		_, err := d.CreateChannelMessage("ch", "sender", "msg", "")
		require.NoError(t, err)
	}

	msgs, err := d.ListChannelMessages("ch", 2)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
}

func TestListChannelMessages_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	msgs, err := d.ListChannelMessages("nonexistent", 10)
	require.NoError(t, err)
	assert.Nil(t, msgs)
}

func TestListChannelMessages_isolatedByChannel(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.CreateChannelMessage("alpha", "s", "alpha-msg", "")
	require.NoError(t, err)
	_, err = d.CreateChannelMessage("beta", "s", "beta-msg", "")
	require.NoError(t, err)

	alphaMsgs, err := d.ListChannelMessages("alpha", 10)
	require.NoError(t, err)
	require.Len(t, alphaMsgs, 1)
	assert.Equal(t, "alpha-msg", alphaMsgs[0].Body)
}

// --- ListChannels ---

func TestListChannels_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	channels, err := d.ListChannels("")
	require.NoError(t, err)
	assert.Nil(t, channels)
}

func TestListChannels_withSubscribers(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.0", "workshop"))
	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.1", "workshop"))

	channels, err := d.ListChannels("")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, "updates", channels[0].Name)
	assert.Equal(t, 2, channels[0].SubscriberCount)
}

func TestListChannels_withMessages(t *testing.T) {
	d := testhelpers.TempDB(t)

	_, err := d.CreateChannelMessage("alerts", "system", "alert1", "workshop")
	require.NoError(t, err)
	_, err = d.CreateChannelMessage("alerts", "system", "alert2", "workshop")
	require.NoError(t, err)

	channels, err := d.ListChannels("")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, "alerts", channels[0].Name)
	assert.Equal(t, 2, channels[0].MessageCount)
}

func TestListChannels_withSubsAndMessages(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("updates", "agent:0.0", "workshop"))
	_, err := d.CreateChannelMessage("updates", "agent:0.0", "hello", "workshop")
	require.NoError(t, err)

	channels, err := d.ListChannels("")
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, "updates", channels[0].Name)
	assert.Equal(t, 1, channels[0].SubscriberCount)
	assert.Equal(t, 1, channels[0].MessageCount)
}

func TestListChannels_multipleChannels(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("alpha", "agent:0.0", ""))
	require.NoError(t, d.CreateChannelSubscription("beta", "agent:0.1", ""))
	_, err := d.CreateChannelMessage("gamma", "s", "msg", "")
	require.NoError(t, err)

	channels, err := d.ListChannels("")
	require.NoError(t, err)
	assert.Len(t, channels, 3)
	// Should be ordered alphabetically
	assert.Equal(t, "alpha", channels[0].Name)
	assert.Equal(t, "beta", channels[1].Name)
	assert.Equal(t, "gamma", channels[2].Name)
}

func TestListChannels_projectFilter(t *testing.T) {
	d := testhelpers.TempDB(t)

	require.NoError(t, d.CreateChannelSubscription("workshop-updates", "agent:0.0", "workshop"))
	require.NoError(t, d.CreateChannelSubscription("roblox-updates", "agent:0.1", "roblox"))

	channels, err := d.ListChannels("workshop")
	require.NoError(t, err)
	// Should include workshop channels and channels with empty project
	found := false
	for _, ch := range channels {
		if ch.Name == "workshop-updates" {
			found = true
		}
		// roblox-only channels should be excluded
		assert.NotEqual(t, "roblox-updates", ch.Name)
	}
	assert.True(t, found, "workshop-updates should be in results")
}
