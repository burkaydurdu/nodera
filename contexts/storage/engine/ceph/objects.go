package ceph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/utils/str"
)

// ListObjects pages one folder: the gateway's continuation token is the cursor.
func (c *Client) ListObjects(ctx context.Context, account *engine.Account, bucket, prefix, cursor string, limit int) (*engine.Listing, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Delimiter: aws.String("/"), MaxKeys: aws.Int32(int32(limit)),
	}
	if str.IsNotEmpty(prefix) {
		input.Prefix = aws.String(prefix)
	}
	if str.IsNotEmpty(cursor) {
		input.ContinuationToken = aws.String(cursor)
	}
	out, err := c.s3For(account).ListObjectsV2(ctx, input)
	if err != nil {
		return nil, wrap("listing objects", err)
	}
	listing := &engine.Listing{Folders: make([]string, 0, len(out.CommonPrefixes)), Objects: make([]engine.Object, 0, len(out.Contents))}
	for _, p := range out.CommonPrefixes {
		listing.Folders = append(listing.Folders, aws.ToString(p.Prefix))
	}
	for _, o := range out.Contents {
		listing.Objects = append(listing.Objects, engine.Object{
			Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size), ModifiedAt: aws.ToTime(o.LastModified),
		})
	}
	if aws.ToBool(out.IsTruncated) {
		listing.Next = aws.ToString(out.NextContinuationToken)
	}

	return listing, nil
}

// DeleteObjects removes the current version of each key, a thousand at a
// time as S3 takes them. A key already gone is success; one the gateway
// refuses fails the call.
func (c *Client) DeleteObjects(ctx context.Context, account *engine.Account, bucket string, keys []string) error {
	client := c.s3For(account)
	for len(keys) > 0 {
		page := keys
		if len(page) > 1000 {
			page = keys[:1000]
		}
		keys = keys[len(page):]
		ids := make([]types.ObjectIdentifier, 0, len(page))
		for _, key := range page {
			ids = append(ids, types.ObjectIdentifier{Key: aws.String(key)})
		}
		out, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket), Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return wrap("deleting objects", err)
		}
		if len(out.Errors) > 0 {
			first := out.Errors[0]

			return fmt.Errorf("deleting objects: %s on %s: %s", aws.ToString(first.Code), aws.ToString(first.Key), aws.ToString(first.Message))
		}
	}

	return nil
}

func (c *Client) PresignDownload(ctx context.Context, account *engine.Account, bucket, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("presigning a download: the URL needs a lifetime")
	}
	out, err := s3.NewPresignClient(c.s3Public(account)).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", wrap("presigning a download", err)
	}

	return out.URL, nil
}

// PresignUpload signs a PUT whose body is not part of the signature, since
// the browser sends it later; the gateway still counts it against the quota.
// The file's type is signed too: a browser always sends one, and the gateway
// answers AccessDenied to a presigned request carrying a type it did not sign.
func (c *Client) PresignUpload(ctx context.Context, account *engine.Account, bucket, key, contentType string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("presigning an upload: the URL needs a lifetime")
	}
	input := &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if str.IsNotEmpty(contentType) {
		input.ContentType = aws.String(contentType)
	}
	out, err := s3.NewPresignClient(c.s3Public(account)).PresignPutObject(ctx, input, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", wrap("presigning an upload", err)
	}

	return out.URL, nil
}

// ObjectAccess reads whether the object's ACL lets everyone read it. The
// bucket's own policy may publish or refuse more; the ACL is what the browser's
// switch sets.
func (c *Client) ObjectAccess(ctx context.Context, account *engine.Account, bucket, key string) (bool, error) {
	acl, err := c.s3For(account).GetObjectAcl(ctx, &s3.GetObjectAclInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		if hasCode(err, "NoSuchKey") {
			return false, engine.ErrNoSuchObject
		}

		return false, wrap("reading the object's access", err)
	}
	for _, grant := range acl.Grants {
		if grant.Grantee != nil && grant.Grantee.Type == types.TypeGroup && aws.ToString(grant.Grantee.URI) == allUsersGroup &&
			(grant.Permission == types.PermissionRead || grant.Permission == types.PermissionFullControl) {
			return true, nil
		}
	}

	return false, nil
}

// SetObjectAccess lets everyone read the object, or takes that back. The
// canned ACLs grant reading alone; writing stays with the keys.
func (c *Client) SetObjectAccess(ctx context.Context, account *engine.Account, bucket, key string, public bool) error {
	acl := types.ObjectCannedACLPrivate
	if public {
		acl = types.ObjectCannedACLPublicRead
	}
	_, err := c.s3For(account).PutObjectAcl(ctx, &s3.PutObjectAclInput{Bucket: aws.String(bucket), Key: aws.String(key), ACL: acl})
	if err != nil {
		if hasCode(err, "NoSuchKey") {
			return engine.ErrNoSuchObject
		}

		return wrap("changing the object's access", err)
	}

	return nil
}

// allUsersGroup is S3's grantee for anyone, signed in or not.
const allUsersGroup = "http://acs.amazonaws.com/groups/global/AllUsers"
