package ceph

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/utils/str"
)

// corsRuleID marks the web app's rule among the bucket's own.
const corsRuleID = "nodera-web-app"

// OpenBucket makes the bucket as the Project's root user, so the account owns
// it, then caps, rate limits and opens it to the browser. Each step holds
// when it already holds, so a crash leaves nothing a second call will not finish.
func (c *Client) OpenBucket(ctx context.Context, account *engine.Account, bucket string, sizeBytes int64) error {
	info, err := c.admin.getBucket(ctx, bucket)
	switch {
	case err == nil && info.Owner != account.ID:
		return fmt.Errorf("bucket %s belongs to another account", bucket)
	case err != nil && !adminNotFound(err):
		return wrap("looking up the bucket", err)
	case err != nil:
		_, err = c.s3For(account).CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
		if err != nil && !hasCode(err, "BucketAlreadyOwnedByYou") {
			return wrap("creating the bucket", err)
		}
	}
	if err := c.SetSizeLimit(ctx, account, bucket, sizeBytes); err != nil {
		return err
	}
	if c.cfg.RateLimit != (RateLimit{}) {
		if err := c.admin.setBucketRateLimit(ctx, bucket, c.cfg.RateLimit); err != nil {
			return wrap("rate limiting the bucket", err)
		}
	}

	return c.AllowBrowser(ctx, account, bucket)
}

func (c *Client) SetSizeLimit(ctx context.Context, _ *engine.Account, bucket string, sizeBytes int64) error {
	// The quota call wants a uid argument even when the bucket is named, and
	// an empty one makes it skip the ownership check that account-owned
	// buckets would fail.
	if err := c.admin.setBucketQuota(ctx, "", bucket, sizeBytes); err != nil {
		return wrap("setting the size limit", err)
	}

	return nil
}

// AllowBrowser puts the web app's CORS rule beside whatever rules the bucket
// has, replacing an earlier copy of it rather than adding a second.
func (c *Client) AllowBrowser(ctx context.Context, account *engine.Account, bucket string) error {
	client := c.s3For(account)
	var rules []types.CORSRule
	current, err := client.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(bucket)})
	switch {
	case err == nil:
		rules = current.CORSRules
	case hasCode(err, "NoSuchCORSConfiguration"):
	default:
		return wrap("reading the bucket's CORS rules", err)
	}
	_, err = client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: &types.CORSConfiguration{CORSRules: mergeCORS(rules, c.webRule())},
	})

	return wrap("writing the bucket's CORS rules", err)
}

func (c *Client) webRule() types.CORSRule {
	return types.CORSRule{
		ID:             aws.String(corsRuleID),
		AllowedOrigins: []string{c.cfg.WebOrigin},
		AllowedMethods: []string{"GET", "PUT", "HEAD"},
		AllowedHeaders: []string{"*"},
		ExposeHeaders:  []string{"ETag"},
		MaxAgeSeconds:  aws.Int32(3600),
	}
}

// mergeCORS answers the rules with the web app's in place of its earlier copy,
// or appended when there was none. The customer's rules are untouched.
func mergeCORS(rules []types.CORSRule, web types.CORSRule) []types.CORSRule {
	merged := make([]types.CORSRule, 0, len(rules)+1)
	replaced := false
	for _, rule := range rules {
		if aws.ToString(rule.ID) == aws.ToString(web.ID) {
			if !replaced {
				merged = append(merged, web)
				replaced = true
			}

			continue
		}
		merged = append(merged, rule)
	}
	if !replaced {
		merged = append(merged, web)
	}

	return merged
}

// Stop writes the reserved Deny statements into the bucket's policy beside
// the customer's own; see policy.go for what they close.
func (c *Client) Stop(ctx context.Context, account *engine.Account, bucket string) error {
	client := c.s3For(account)
	current, err := c.bucketPolicy(ctx, client, bucket)
	if err != nil {
		return err
	}
	stopped, err := stoppedPolicy(current, bucket, account.ID)
	if err != nil {
		return fmt.Errorf("stopping the bucket: %w", err)
	}
	_, err = client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(bucket), Policy: aws.String(stopped)})

	return wrap("stopping the bucket", err)
}

// Resume removes exactly the reserved statements and leaves the rest of the
// policy as the customer wrote it; a bucket that had no policy gets none.
func (c *Client) Resume(ctx context.Context, account *engine.Account, bucket string) error {
	client := c.s3For(account)
	current, err := c.bucketPolicy(ctx, client, bucket)
	if err != nil {
		return err
	}
	if !isStopped(current) {
		return nil
	}
	resumed, err := resumedPolicy(current)
	if err != nil {
		return fmt.Errorf("resuming the bucket: %w", err)
	}
	if str.IsEmpty(resumed) {
		_, err = client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
	} else {
		_, err = client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(bucket), Policy: aws.String(resumed)})
	}

	return wrap("resuming the bucket", err)
}

// bucketPolicy answers the policy document as the gateway holds it, empty
// when the bucket has none.
func (c *Client) bucketPolicy(ctx context.Context, client *s3.Client, bucket string) (string, error) {
	out, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	switch {
	case err == nil:
		return aws.ToString(out.Policy), nil
	case hasCode(err, "NoSuchBucketPolicy"):
		return "", nil
	default:
		return "", wrap("reading the bucket's policy", err)
	}
}

// DeleteBucket first takes the bucket's Access keys away and stops it, so
// nothing is written while the objects go, then removes every version and
// unfinished upload and the bucket itself. A bucket already gone is success.
func (c *Client) DeleteBucket(ctx context.Context, account *engine.Account, bucket string) error {
	if err := c.deleteBucketUsers(ctx, account, bucket); err != nil {
		return err
	}
	info, err := c.admin.getBucket(ctx, bucket)
	switch {
	case adminNotFound(err):
		return nil
	case err != nil:
		return wrap("looking up the bucket", err)
	case info.Owner != account.ID:
		return fmt.Errorf("bucket %s belongs to another account", bucket)
	}
	if err := c.Stop(ctx, account, bucket); err != nil {
		return err
	}
	client := c.s3For(account)
	if err := c.abortUploads(ctx, client, bucket); err != nil {
		return err
	}
	if err := c.purgeVersions(ctx, client, bucket); err != nil {
		return err
	}
	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(bucket)})
	if err != nil && !hasCode(err, "NoSuchBucket") {
		return wrap("deleting the bucket", err)
	}

	return nil
}

func (c *Client) abortUploads(ctx context.Context, client *s3.Client, bucket string) error {
	var keyMarker, idMarker *string
	for {
		uploads, err := client.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{
			Bucket: aws.String(bucket), KeyMarker: keyMarker, UploadIdMarker: idMarker,
		})
		if err != nil {
			return wrap("listing unfinished uploads", err)
		}
		for _, upload := range uploads.Uploads {
			_, err := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket: aws.String(bucket), Key: upload.Key, UploadId: upload.UploadId,
			})
			if err != nil && !hasCode(err, "NoSuchUpload") {
				return wrap("aborting an unfinished upload", err)
			}
		}
		if !aws.ToBool(uploads.IsTruncated) {
			return nil
		}
		keyMarker, idMarker = uploads.NextKeyMarker, uploads.NextUploadIdMarker
	}
}

func (c *Client) purgeVersions(ctx context.Context, client *s3.Client, bucket string) error {
	for {
		versions, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), MaxKeys: aws.Int32(1000)})
		if err != nil {
			return wrap("listing the bucket's versions", err)
		}
		var ids []types.ObjectIdentifier
		for _, v := range versions.Versions {
			ids = append(ids, types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
		}
		for _, m := range versions.DeleteMarkers {
			ids = append(ids, types.ObjectIdentifier{Key: m.Key, VersionId: m.VersionId})
		}
		if len(ids) == 0 {
			return nil
		}
		deleted, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket), Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return wrap("deleting the bucket's versions", err)
		}
		// A version that will not go would be listed again forever.
		if len(deleted.Errors) > 0 {
			first := deleted.Errors[0]

			return fmt.Errorf("deleting the bucket's versions: %s on %s: %s", aws.ToString(first.Code), aws.ToString(first.Key), aws.ToString(first.Message))
		}
	}
}
