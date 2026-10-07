package ceph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/piggystats/nodera/contexts/storage/engine"
)

// The Stop is two reserved statements in the bucket's own policy, each with a
// Sid no customer would write. They stay in place beside whatever the customer
// set and are the only thing Resume removes.
//
// stopSid denies everyone, the Project's root user included, everything but
// what the adapter itself still needs: so a presigned URL handed out before
// the Stop dies with it, and no Access key, published object or bucket policy
// of the customer's reads or writes a byte. stopRootSid denies those remaining
// actions to everyone but the root user, so the adapter can still list, purge
// and delete a stopped bucket and keep its CORS rule, while an Access key
// cannot. The gateway lets the owning account's root user read and change the
// bucket policy whatever the policy says, which is what makes Resume possible.
const (
	stopSid     = "nodera-stop"
	stopRootSid = "nodera-stop-root"
	// rootDisplayName is the Project root user's name and so the principal
	// stopRootSid exempts; an Access key is named after its own id.
	rootDisplayName = "root"
)

// rootKeeps are the actions root goes on doing to a stopped bucket.
var rootKeeps = []string{
	"s3:ListBucket", "s3:ListBucketVersions", "s3:ListBucketMultipartUploads", "s3:ListMultipartUploadParts",
	"s3:DeleteObject", "s3:DeleteObjectVersion", "s3:AbortMultipartUpload",
	"s3:DeleteBucket", "s3:GetBucketCORS", "s3:PutBucketCORS",
	"s3:GetBucketPolicy", "s3:PutBucketPolicy", "s3:DeleteBucketPolicy",
	"s3:GetBucketAcl", "s3:PutBucketAcl",
}

// readActions is what a read key may do: get and list, versions included.
var readActions = []string{
	"s3:GetObject", "s3:GetObjectVersion", "s3:GetObjectAcl", "s3:GetObjectVersionAcl",
	"s3:GetObjectTagging", "s3:GetObjectVersionTagging", "s3:GetObjectAttributes",
	"s3:ListBucket", "s3:ListBucketVersions", "s3:ListBucketMultipartUploads", "s3:ListMultipartUploadParts",
	"s3:GetBucketLocation", "s3:GetBucketVersioning", "s3:GetBucketCORS", "s3:GetBucketAcl",
	"s3:GetBucketPolicy", "s3:GetBucketTagging", "s3:GetLifecycleConfiguration",
}

// statement is one IAM policy statement as the adapter writes it. Fields are
// pointers or slices so that unused ones vanish from the document.
type statement struct {
	Sid          string          `json:"Sid,omitempty"`
	Effect       string          `json:"Effect"`
	Principal    json.RawMessage `json:"Principal,omitempty"`
	NotPrincipal json.RawMessage `json:"NotPrincipal,omitempty"`
	Action       []string        `json:"Action,omitempty"`
	NotAction    []string        `json:"NotAction,omitempty"`
	Resource     []string        `json:"Resource"`
}

type policyDocument struct {
	Version   string      `json:"Version"`
	Statement []statement `json:"Statement"`
}

func bucketResources(bucket string) []string {
	return []string{"arn:aws:s3:::" + bucket, "arn:aws:s3:::" + bucket + "/*"}
}

// keyPolicy is the one policy of an Access key's IAM user. A read_write key
// gets every S3 action on the bucket and its objects except making or
// deleting a bucket, which is a purchase and happens only through Nodera
// (docs/adr/0012); listing the account's buckets is withheld as well so a key
// learns nothing of the Project's other Storages.
func keyPolicy(bucket string, permission engine.Permission) (string, error) {
	var statements []statement
	switch permission {
	case engine.Read:
		statements = []statement{{Effect: "Allow", Action: readActions, Resource: bucketResources(bucket)}}
	case engine.ReadWrite:
		statements = []statement{
			{Effect: "Allow", Action: []string{"s3:*"}, Resource: bucketResources(bucket)},
			{Effect: "Deny", Action: []string{"s3:CreateBucket", "s3:DeleteBucket"}, Resource: []string{"*"}},
		}
	default:
		return "", fmt.Errorf("unknown permission %q", permission)
	}
	raw, err := json.Marshal(policyDocument{Version: "2012-10-17", Statement: statements})
	if err != nil {
		return "", err
	}

	return string(raw), nil
}

func stopStatements(bucket, accountID string) []statement {
	return []statement{
		// The gateway reads NotAction as a subtraction from Action and never
		// on its own, so the whole of S3 is named first.
		{
			Sid: stopSid, Effect: "Deny", Principal: json.RawMessage(`{"AWS":["*"]}`),
			Action: []string{"s3:*"}, NotAction: rootKeeps, Resource: bucketResources(bucket),
		},
		{
			Sid: stopRootSid, Effect: "Deny",
			NotPrincipal: json.RawMessage(fmt.Sprintf(`{"AWS":["arn:aws:iam::%s:user/%s"]}`, accountID, rootDisplayName)),
			Action:       rootKeeps, Resource: bucketResources(bucket),
		},
	}
}

// genericPolicy keeps a customer's policy as it was written: statements are
// kept raw so nothing of theirs is reshaped, and the document's other fields
// (Id, Version) ride along.
type genericPolicy struct {
	fields     map[string]json.RawMessage
	statements []json.RawMessage
}

func parsePolicy(raw string) (*genericPolicy, error) {
	p := &genericPolicy{fields: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace([]byte(raw))) == 0 {
		return p, nil
	}
	if err := json.Unmarshal([]byte(raw), &p.fields); err != nil {
		return nil, fmt.Errorf("bucket policy is not a JSON object: %w", err)
	}
	statements, ok := p.fields["Statement"]
	if !ok {
		return p, nil
	}
	delete(p.fields, "Statement")
	trimmed := bytes.TrimSpace(statements)
	// A single statement may be written without the surrounding array.
	if len(trimmed) > 0 && trimmed[0] == '{' {
		p.statements = []json.RawMessage{statements}

		return p, nil
	}
	if err := json.Unmarshal(statements, &p.statements); err != nil {
		return nil, fmt.Errorf("bucket policy Statement is neither an object nor an array: %w", err)
	}

	return p, nil
}

func statementSid(raw json.RawMessage) string {
	var s struct {
		Sid string `json:"Sid"`
	}
	_ = json.Unmarshal(raw, &s)

	return s.Sid
}

func (p *genericPolicy) hasSid(sid string) bool {
	for _, s := range p.statements {
		if statementSid(s) == sid {
			return true
		}
	}

	return false
}

// withoutSids drops the statements that carry one of the reserved Sids and
// nothing else.
func (p *genericPolicy) withoutSids(sids ...string) {
	kept := p.statements[:0:0]
	for _, s := range p.statements {
		reserved := false
		for _, sid := range sids {
			if statementSid(s) == sid {
				reserved = true
			}
		}
		if !reserved {
			kept = append(kept, s)
		}
	}
	p.statements = kept
}

func (p *genericPolicy) add(statements ...statement) error {
	for _, s := range statements {
		raw, err := json.Marshal(s)
		if err != nil {
			return err
		}
		p.statements = append(p.statements, raw)
	}

	return nil
}

func (p *genericPolicy) empty() bool { return len(p.statements) == 0 }

// String renders the document; the keys the customer wrote keep a stable order.
func (p *genericPolicy) String() (string, error) {
	keys := make([]string, 0, len(p.fields))
	for k := range p.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteByte('{')
	if _, ok := p.fields["Version"]; !ok {
		buf.WriteString(`"Version":"2012-10-17",`)
	}
	for _, k := range keys {
		name, _ := json.Marshal(k)
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(p.fields[k])
		buf.WriteByte(',')
	}
	statements, err := json.Marshal(p.statements)
	if err != nil {
		return "", err
	}
	buf.WriteString(`"Statement":`)
	buf.Write(statements)
	buf.WriteByte('}')

	return buf.String(), nil
}

// stoppedPolicy adds the reserved statements to the customer's policy, once.
func stoppedPolicy(current, bucket, accountID string) (string, error) {
	p, err := parsePolicy(current)
	if err != nil {
		return "", err
	}
	p.withoutSids(stopSid, stopRootSid)
	if err := p.add(stopStatements(bucket, accountID)...); err != nil {
		return "", err
	}

	return p.String()
}

// resumedPolicy removes exactly the reserved statements. An empty answer
// means the bucket had no policy of its own and should have none again.
func resumedPolicy(current string) (string, error) {
	p, err := parsePolicy(current)
	if err != nil {
		return "", err
	}
	p.withoutSids(stopSid, stopRootSid)
	if p.empty() {
		return "", nil
	}

	return p.String()
}

func isStopped(current string) bool {
	p, err := parsePolicy(current)
	if err != nil {
		return false
	}

	return p.hasSid(stopSid)
}
