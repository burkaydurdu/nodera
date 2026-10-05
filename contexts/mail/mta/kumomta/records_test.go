package kumomta_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/aggregates/message"
	"github.com/piggystats/nodera/contexts/mail/mta"
	"github.com/piggystats/nodera/contexts/mail/mta/kumomta"
)

const id = "0b1d2c3e-4f50-4a6b-8c7d-9e0f1a2b3c4d"

var paths = message.NewReturnPaths([]byte("records-test-key"))

func parse(t *testing.T, raw string) []mta.Outcome {
	t.Helper()
	outcomes, err := kumomta.NewRecords(paths).Parse([]byte(raw))
	require.NoError(t, err)

	return outcomes
}

func TestRecords_Delivery(t *testing.T) {
	outcomes := parse(t, `{"type":"Delivery","id":"k1","sender":"b+`+id+`.2.000000000000@n4k2x9q1z.example.com",
		"recipient":"Ana@Example.net","event_time":"2026-10-05T12:00:03Z","timestamp":1759665603,
		"response":{"code":250,"enhanced_code":{"class":2,"subject":0,"detail":0},"content":"OK queued","command":null},
		"bounce_classification":"Uncategorized","meta":{"x_message_id":"`+id+`","x_position":"2","tenant":"p"}}`)
	require.Len(t, outcomes, 1)

	o := outcomes[0]
	assert.Equal(t, uuid.MustParse(id), o.MessageID)
	assert.Equal(t, 2, o.Position)
	assert.Equal(t, message.EventDelivered, o.Type)
	assert.Equal(t, time.Date(2026, 10, 5, 12, 0, 3, 0, time.UTC), o.OccurredAt)
	assert.Equal(t, &message.Response{Code: 250, EnhancedCode: "2.0.0", Text: "OK queued"}, o.Response)
	assert.False(t, o.Permanent)
}

func TestRecords_PermanentBounceOnlyForABadAddress(t *testing.T) {
	cases := []struct {
		classification string
		enhanced       string
		permanent      bool
	}{
		{"InvalidRecipient", `{"class":5,"subject":1,"detail":1}`, true},
		{"Uncategorized", `{"class":5,"subject":1,"detail":1}`, true},
		{"BadDomain", `null`, true},
		{"SpamBlock", `{"class":5,"subject":7,"detail":1}`, false},
		{"PolicyRelated", `{"class":5,"subject":7,"detail":0}`, false},
	}

	for _, c := range cases {
		outcomes := parse(t, `{"type":"Bounce","recipient":"ana@example.net","timestamp":1759665603,
			"response":{"code":550,"enhanced_code":`+c.enhanced+`,"content":"no"},"bounce_classification":"`+c.classification+`",
			"meta":{"x_message_id":"`+id+`","x_position":"0"}}`)
		require.Len(t, outcomes, 1)
		assert.Equal(t, message.EventBounced, outcomes[0].Type)
		assert.Equalf(t, c.permanent, outcomes[0].Permanent, c.classification)
	}
}

func TestRecords_TransientAndExpiration(t *testing.T) {
	outcomes := parse(t, `[
		{"type":"TransientFailure","recipient":"ana@example.net","timestamp":1759665603,"response":{"code":451,"enhanced_code":null,"content":"later"},"meta":{"x_message_id":"`+id+`","x_position":"0"}},
		{"type":"Expiration","recipient":"ana@example.net","timestamp":1759752003,"response":{"code":551,"enhanced_code":null,"content":"expired"},"meta":{"x_message_id":"`+id+`","x_position":"0"}}
	]`)
	require.Len(t, outcomes, 2)
	assert.Equal(t, message.EventDeferred, outcomes[0].Type)
	assert.Equal(t, message.EventBounced, outcomes[1].Type)
	assert.Equal(t, "MessageExpired", outcomes[1].BounceClass)
	assert.False(t, outcomes[1].Permanent)
}

// An asynchronous bounce carries none of the Message's metadata: its sender
// is the return path the recipient's copy went out with.
func TestRecords_OOBFindsTheRecipientInTheReturnPath(t *testing.T) {
	returnPath := paths.For(uuid.MustParse(id), 1, "n4k2x9q1z.example.com")

	outcomes := parse(t, `{"type":"OOB","sender":"`+returnPath+`","recipient":["forwarded@elsewhere.example"],
		"timestamp":1759665603,"response":{"code":550,"enhanced_code":{"class":5,"subject":1,"detail":1},"content":"user unknown"},
		"bounce_classification":"InvalidRecipient","meta":{}}`)

	require.Len(t, outcomes, 1)
	assert.Equal(t, uuid.MustParse(id), outcomes[0].MessageID)
	assert.Equal(t, 1, outcomes[0].Position, "a forwarded address bounces under another name")
	assert.Equal(t, message.EventBounced, outcomes[0].Type)
	assert.True(t, outcomes[0].Permanent)
}

func TestRecords_OOBToAReturnPathNoderaDidNotMakeIsDropped(t *testing.T) {
	forged := "b+" + id + ".1.000000000000@n4k2x9q1z.example.com"

	outcomes := parse(t, `{"type":"OOB","sender":"`+forged+`","recipient":["bob@example.net"],"timestamp":1759665603,
		"response":{"code":550,"enhanced_code":{"class":5,"subject":1,"detail":1},"content":"user unknown"},
		"bounce_classification":"InvalidRecipient","meta":{}}`)

	assert.Empty(t, outcomes)
}

func TestRecords_FeedbackIsAComplaintAboutTheCopyItTraces(t *testing.T) {
	returnPath := paths.For(uuid.MustParse(id), 2, "n4k2x9q1z.example.com")

	outcomes := parse(t, `{"type":"Feedback","recipient":"fbl@rp.mta.nodera.io","timestamp":1759665603,
		"response":{"code":250,"enhanced_code":null,"content":""},"meta":{},
		"feedback_report":{"feedback_type":"abuse","version":"1","original_rcpto_to":["redacted@example.net"],
			"supplemental_trace":{"x_message_id":"`+id+`","x_return_path":"`+returnPath+`"}}}`)

	require.Len(t, outcomes, 1)
	assert.Equal(t, message.EventComplained, outcomes[0].Type)
	assert.Equal(t, uuid.MustParse(id), outcomes[0].MessageID)
	assert.Equal(t, 2, outcomes[0].Position)
}

func TestRecords_IgnoresRecordsThatAreNotOutcomesOrNotOurs(t *testing.T) {
	outcomes := parse(t, `[
		{"type":"Reception","recipient":"ana@example.net","timestamp":1759665603,"meta":{"x_message_id":"`+id+`","x_position":"0"}},
		{"type":"Delivered","recipient":"ana@example.net","timestamp":1759665603,"meta":{"x_message_id":"`+id+`","x_position":"0"}},
		{"type":"Delivery","recipient":"ana@example.net","timestamp":1759665603,"meta":{}},
		{"type":"Delivery","recipient":"ana@example.net","timestamp":1759665603,"meta":{"x_message_id":"`+id+`"}}
	]`)

	assert.Empty(t, outcomes)
}

func TestRecords_RefusesWhatIsNotJSON(t *testing.T) {
	_, err := kumomta.NewRecords(paths).Parse([]byte(`not json`))

	assert.Error(t, err)
}
