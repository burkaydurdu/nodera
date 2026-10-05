package sendingdomain_test

import (
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/mail/aggregates/sendingdomain"
)

func sendingDomain() *sendingdomain.SendingDomain {
	created := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	return &sendingdomain.SendingDomain{Name: "example.com", Token: "4k2x9q1z", Status: sendingdomain.StatusPending, ActiveSelector: sendingdomain.SelectorA, CreatedAt: created}
}

func TestRecords_AreTheThreeCNAMEsOfTheSpec(t *testing.T) {
	records := sendingDomain().Records(zone)

	require.Len(t, records, 3)
	assert.Equal(t, sendingdomain.Record{Purpose: sendingdomain.PurposeReturnPath, Type: "CNAME", Host: "n4k2x9q1z.example.com", Value: "rp.mta.nodera.io"}, records[0])
	assert.Equal(t, sendingdomain.Record{Purpose: sendingdomain.PurposeDKIM, Type: "CNAME", Host: "n4k2x9q1za._domainkey.example.com", Value: "a.4k2x9q1z.dkim.mta.nodera.io"}, records[1])
	assert.Equal(t, sendingdomain.Record{Purpose: sendingdomain.PurposeDKIM, Type: "CNAME", Host: "n4k2x9q1zb._domainkey.example.com", Value: "b.4k2x9q1z.dkim.mta.nodera.io"}, records[2])
}

func TestSigner_UsesTheActiveSelector(t *testing.T) {
	d := sendingDomain()
	assert.Equal(t, "n4k2x9q1za", d.SigningSelector())

	d.ActiveSelector = sendingdomain.SelectorB
	assert.Equal(t, "n4k2x9q1zb", d.SigningSelector())
	assert.Equal(t, "n4k2x9q1z.example.com", d.ReturnPathDomain())
}

func TestApplyCheck_ADomainMissingARecordStaysPending(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 30, 0, 0, time.UTC)
	d := sendingDomain()

	change := d.ApplyCheck(sendingdomain.CheckResult{ReturnPath: true, DKIMA: true, DKIMB: false}, now)

	assert.Equal(t, sendingdomain.ChangeNone, change)
	assert.Equal(t, sendingdomain.StatusPending, d.Status)
	assert.Equal(t, now.Add(10*time.Minute), d.NextCheckAt, "a new domain is checked every ten minutes")
}

func TestApplyCheck_AllThreeRecordsVerify(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 30, 0, 0, time.UTC)
	d := sendingDomain()

	change := d.ApplyCheck(sendingdomain.CheckResult{ReturnPath: true, DKIMA: true, DKIMB: true, DMARC: true}, now)

	assert.Equal(t, sendingdomain.ChangeVerified, change)
	assert.Equal(t, sendingdomain.StatusVerified, d.Status)
	assert.Equal(t, now, *d.VerifiedAt)
	assert.True(t, d.DMARCFound)
	assert.Equal(t, now.Add(24*time.Hour), d.NextCheckAt)
}

func TestApplyCheck_ACheckDNSDidNotAnswerChangesNothingAndComesBackSoon(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	d := sendingDomain()
	d.ApplyCheck(sendingdomain.CheckResult{ReturnPath: true, DKIMA: true, DKIMB: true}, now)

	change := d.ApplyCheck(sendingdomain.CheckResult{Unanswered: true}, now.Add(25*time.Hour))

	assert.Equal(t, sendingdomain.ChangeNone, change)
	assert.Equal(t, sendingdomain.StatusVerified, d.Status)
	assert.True(t, d.ReturnPathValid && d.DKIMAValid && d.DKIMBValid)
	assert.Equal(t, now, *d.LastCheckedAt, "a check without an answer is no check")
	assert.Equal(t, now.Add(25*time.Hour+10*time.Minute), d.NextCheckAt)
}

func TestApplyCheck_LosesAndRegainsVerification(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	d := sendingDomain()
	d.ApplyCheck(sendingdomain.CheckResult{ReturnPath: true, DKIMA: true, DKIMB: true}, now)

	assert.Equal(t, sendingdomain.ChangeUnverified, d.ApplyCheck(sendingdomain.CheckResult{ReturnPath: true, DKIMA: false, DKIMB: true}, now.Add(time.Hour)))
	assert.Equal(t, sendingdomain.StatusUnverified, d.Status)
	assert.Equal(t, sendingdomain.ChangeNone, d.ApplyCheck(sendingdomain.CheckResult{}, now.Add(2*time.Hour)))
	assert.Equal(t, sendingdomain.ChangeVerified, d.ApplyCheck(sendingdomain.CheckResult{ReturnPath: true, DKIMA: true, DKIMB: true}, now.Add(3*time.Hour)))
}

func TestApplyCheck_SlowsDownAPendingDomainAfterThreeDays(t *testing.T) {
	d := sendingDomain()
	later := d.CreatedAt.Add(73 * time.Hour)

	d.ApplyCheck(sendingdomain.CheckResult{}, later)
	assert.Equal(t, later.Add(24*time.Hour), d.NextCheckAt)
}

func TestGenerateKey_PublishesTheKeyItSigns(t *testing.T) {
	pair, err := sendingdomain.GenerateKey()
	require.NoError(t, err)

	private, err := x509.ParsePKCS8PrivateKey(pair.PrivatePKCS8)
	require.NoError(t, err)
	der, err := base64.StdEncoding.DecodeString(pair.PublicKey)
	require.NoError(t, err)
	public, err := x509.ParsePKIXPublicKey(der)
	require.NoError(t, err)

	assert.Equal(t, private.(crypto.Signer).Public(), public)

	value := sendingdomain.DKIMRecord(pair.PublicKey)
	assert.True(t, strings.HasPrefix(value, "v=DKIM1; k=rsa; p="))
}
