package ceph

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/storage/engine"
)

func decode(t *testing.T, raw string) policyDocument {
	t.Helper()
	var doc policyDocument
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))

	return doc
}

func TestKeyPolicyReadAllowsGettingAndListingOnly(t *testing.T) {
	raw, err := keyPolicy("photos", engine.Read)
	require.NoError(t, err)
	doc := decode(t, raw)

	require.Len(t, doc.Statement, 1)
	assert.Equal(t, "Allow", doc.Statement[0].Effect)
	assert.Equal(t, []string{"arn:aws:s3:::photos", "arn:aws:s3:::photos/*"}, doc.Statement[0].Resource)
	assert.Contains(t, doc.Statement[0].Action, "s3:GetObject")
	assert.Contains(t, doc.Statement[0].Action, "s3:ListBucket")
	for _, action := range doc.Statement[0].Action {
		assert.NotContains(t, []string{"s3:PutObject", "s3:DeleteObject", "s3:*", "s3:CreateBucket", "s3:DeleteBucket", "s3:PutBucketPolicy"}, action)
	}
}

func TestKeyPolicyReadWriteAllowsEverythingButBuckets(t *testing.T) {
	raw, err := keyPolicy("photos", engine.ReadWrite)
	require.NoError(t, err)
	doc := decode(t, raw)

	require.Len(t, doc.Statement, 2)
	assert.Equal(t, "Allow", doc.Statement[0].Effect)
	assert.Equal(t, []string{"s3:*"}, doc.Statement[0].Action)
	assert.Equal(t, []string{"arn:aws:s3:::photos", "arn:aws:s3:::photos/*"}, doc.Statement[0].Resource)
	assert.Equal(t, "Deny", doc.Statement[1].Effect)
	assert.ElementsMatch(t, []string{"s3:CreateBucket", "s3:DeleteBucket"}, doc.Statement[1].Action)
	assert.Equal(t, []string{"*"}, doc.Statement[1].Resource)
}

func TestKeyPolicyRefusesAnUnknownPermission(t *testing.T) {
	_, err := keyPolicy("photos", engine.Permission("admin"))
	assert.Error(t, err)
}

// The customer's policy, with a Statement written as a single object and an
// Id, the way some tools write them.
const customerPolicy = `{"Version":"2012-10-17","Id":"site","Statement":{"Sid":"public","Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::photos/public/*"}}`

func TestStoppedPolicyAddsTheReservedStatementsBesideTheCustomers(t *testing.T) {
	stopped, err := stoppedPolicy(customerPolicy, "photos", "RGW00000000000000001")
	require.NoError(t, err)
	var doc struct {
		ID         string            `json:"Id"`
		Statements []json.RawMessage `json:"Statement"`
	}
	require.NoError(t, json.Unmarshal([]byte(stopped), &doc))
	require.Len(t, doc.Statements, 3)
	assert.Equal(t, "site", doc.ID, "the customer's other fields ride along")
	assert.JSONEq(t, `{"Sid":"public","Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::photos/public/*"}`, string(doc.Statements[0]), "the customer's statement is kept as written")

	var all, root statement
	require.NoError(t, json.Unmarshal(doc.Statements[1], &all))
	require.NoError(t, json.Unmarshal(doc.Statements[2], &root))
	assert.Equal(t, stopSid, all.Sid)
	assert.Equal(t, "Deny", all.Effect)
	assert.Equal(t, []string{"s3:*"}, all.Action, "the gateway subtracts NotAction from Action and never reads it alone")
	assert.Contains(t, all.NotAction, "s3:DeleteObjectVersion")
	assert.JSONEq(t, `{"AWS":["*"]}`, string(all.Principal))
	assert.Equal(t, []string{"arn:aws:s3:::photos", "arn:aws:s3:::photos/*"}, all.Resource)
	assert.Equal(t, stopRootSid, root.Sid)
	assert.Equal(t, "Deny", root.Effect)
	assert.JSONEq(t, `{"AWS":["arn:aws:iam::RGW00000000000000001:user/root"]}`, string(root.NotPrincipal))
	assert.Equal(t, all.NotAction, root.Action, "what the first leaves open the second closes to all but root")
	assert.True(t, isStopped(stopped))
}

func TestStoppedPolicyIsAppliedOnce(t *testing.T) {
	once, err := stoppedPolicy(customerPolicy, "photos", "RGW00000000000000001")
	require.NoError(t, err)
	twice, err := stoppedPolicy(once, "photos", "RGW00000000000000001")
	require.NoError(t, err)

	assert.JSONEq(t, once, twice)
}

func TestResumedPolicyRemovesExactlyTheReservedStatements(t *testing.T) {
	stopped, err := stoppedPolicy(customerPolicy, "photos", "RGW00000000000000001")
	require.NoError(t, err)
	resumed, err := resumedPolicy(stopped)
	require.NoError(t, err)

	assert.JSONEq(t, `{"Version":"2012-10-17","Id":"site","Statement":[{"Sid":"public","Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::photos/public/*"}]}`, resumed)
	assert.False(t, isStopped(resumed))
}

func TestResumedPolicyIsEmptyWhenTheBucketHadNone(t *testing.T) {
	stopped, err := stoppedPolicy("", "photos", "RGW00000000000000001")
	require.NoError(t, err)
	assert.True(t, isStopped(stopped))

	resumed, err := resumedPolicy(stopped)
	require.NoError(t, err)
	assert.Empty(t, resumed)
}

func TestResumedPolicyLeavesARunningPolicyAlone(t *testing.T) {
	resumed, err := resumedPolicy(customerPolicy)
	require.NoError(t, err)

	assert.JSONEq(t, `{"Version":"2012-10-17","Id":"site","Statement":[{"Sid":"public","Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::photos/public/*"}]}`, resumed)
}

func TestIsStoppedIgnoresAnUnreadablePolicy(t *testing.T) {
	assert.False(t, isStopped("not json"))
	assert.False(t, isStopped(""))
}

func TestStoppedPolicyRefusesAnUnreadablePolicy(t *testing.T) {
	_, err := stoppedPolicy("[1,2]", "photos", "RGW00000000000000001")
	assert.Error(t, err)
}
