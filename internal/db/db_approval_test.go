package db_test

import (
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/testhelpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateApproval_happyPath(t *testing.T) {
	d := testhelpers.TempDB(t)

	req := &db.ApprovalRequest{
		PaneTarget: "agent:0.0",
		AgentName:  "orchestrator",
		Action:     "proceed_to_implement",
		Details:    "Plan complete. Review and approve.",
		Diff:       "diff --git a/main.go ...",
		Project:    "workshop",
	}
	id, err := d.CreateApproval(req)
	require.NoError(t, err)
	assert.NotZero(t, id)
}

func TestCreateApproval_minimalFields(t *testing.T) {
	d := testhelpers.TempDB(t)

	req := &db.ApprovalRequest{
		Action:  "deploy",
		Details: "Deploy to production",
	}
	id, err := d.CreateApproval(req)
	require.NoError(t, err)
	assert.NotZero(t, id)
}

func TestResolveApproval_approved(t *testing.T) {
	d := testhelpers.TempDB(t)

	req := &db.ApprovalRequest{Action: "deploy", Details: "Deploy v2"}
	id, err := d.CreateApproval(req)
	require.NoError(t, err)

	err = d.ResolveApproval(id, "approved")
	require.NoError(t, err)

	// Verify via list
	reqs, err := d.ListApprovals("approved", 10)
	require.NoError(t, err)
	require.Len(t, reqs, 1)
	assert.Equal(t, "approved", reqs[0].Status)
	assert.NotEmpty(t, reqs[0].DecidedAt)
}

func TestResolveApproval_denied(t *testing.T) {
	d := testhelpers.TempDB(t)

	req := &db.ApprovalRequest{Action: "deploy", Details: "Deploy v2"}
	id, err := d.CreateApproval(req)
	require.NoError(t, err)

	err = d.ResolveApproval(id, "denied")
	require.NoError(t, err)

	reqs, err := d.ListApprovals("denied", 10)
	require.NoError(t, err)
	require.Len(t, reqs, 1)
	assert.Equal(t, "denied", reqs[0].Status)
}

func TestResolveApproval_invalidDecision(t *testing.T) {
	d := testhelpers.TempDB(t)

	req := &db.ApprovalRequest{Action: "deploy", Details: "Deploy v2"}
	id, err := d.CreateApproval(req)
	require.NoError(t, err)

	err = d.ResolveApproval(id, "maybe")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid decision")
}

func TestResolveApproval_alreadyResolved(t *testing.T) {
	d := testhelpers.TempDB(t)

	req := &db.ApprovalRequest{Action: "deploy", Details: "Deploy v2"}
	id, err := d.CreateApproval(req)
	require.NoError(t, err)

	require.NoError(t, d.ResolveApproval(id, "approved"))

	// Resolving again should fail
	err = d.ResolveApproval(id, "denied")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or already resolved")
}

func TestResolveApproval_nonExistentID(t *testing.T) {
	d := testhelpers.TempDB(t)

	err := d.ResolveApproval(9999, "approved")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or already resolved")
}

func TestListApprovals_pendingFilter(t *testing.T) {
	d := testhelpers.TempDB(t)

	req1 := &db.ApprovalRequest{Action: "deploy", Details: "v1"}
	id1, err := d.CreateApproval(req1)
	require.NoError(t, err)

	req2 := &db.ApprovalRequest{Action: "deploy", Details: "v2"}
	_, err = d.CreateApproval(req2)
	require.NoError(t, err)

	// Approve the first one
	require.NoError(t, d.ResolveApproval(id1, "approved"))

	// Only one pending
	pending, err := d.ListApprovals("pending", 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, "v2", pending[0].Details)
}

func TestListApprovals_noFilter(t *testing.T) {
	d := testhelpers.TempDB(t)

	req1 := &db.ApprovalRequest{Action: "deploy", Details: "v1"}
	id1, err := d.CreateApproval(req1)
	require.NoError(t, err)

	req2 := &db.ApprovalRequest{Action: "deploy", Details: "v2"}
	_, err = d.CreateApproval(req2)
	require.NoError(t, err)

	require.NoError(t, d.ResolveApproval(id1, "denied"))

	// No status filter returns all
	all, err := d.ListApprovals("", 10)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestListApprovals_empty(t *testing.T) {
	d := testhelpers.TempDB(t)

	reqs, err := d.ListApprovals("pending", 10)
	require.NoError(t, err)
	assert.Nil(t, reqs)
}

func TestListApprovals_defaultLimit(t *testing.T) {
	d := testhelpers.TempDB(t)

	// limit <= 0 defaults to 50
	reqs, err := d.ListApprovals("", 0)
	require.NoError(t, err)
	assert.Nil(t, reqs) // no data, just verifying no error
}

func TestListApprovals_allFieldsRoundTrip(t *testing.T) {
	d := testhelpers.TempDB(t)

	req := &db.ApprovalRequest{
		PaneTarget: "agent:0.0",
		AgentName:  "orchestrator",
		Action:     "proceed_to_test",
		Details:    "Implementation complete",
		Diff:       "diff --git a/foo.go b/foo.go\n+func Bar() {}",
		Project:    "workshop",
	}
	_, err := d.CreateApproval(req)
	require.NoError(t, err)

	reqs, err := d.ListApprovals("pending", 10)
	require.NoError(t, err)
	require.Len(t, reqs, 1)

	got := reqs[0]
	assert.Equal(t, "agent:0.0", got.PaneTarget)
	assert.Equal(t, "orchestrator", got.AgentName)
	assert.Equal(t, "proceed_to_test", got.Action)
	assert.Equal(t, "Implementation complete", got.Details)
	assert.Equal(t, "diff --git a/foo.go b/foo.go\n+func Bar() {}", got.Diff)
	assert.Equal(t, "workshop", got.Project)
	assert.Equal(t, "pending", got.Status)
	assert.NotEmpty(t, got.CreatedAt)
}
