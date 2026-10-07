package ceph_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/piggystats/nodera/contexts/storage/application/handlers"
	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/contexts/storage/engine/ceph"
)

const (
	cephImage = "quay.io/ceph/ceph"
	cephTag   = "v20.2.4"

	adminAccessKey = "contract-admin"
	adminSecretKey = "contract-admin-secret"
	region         = "vno1"
	webOrigin      = "http://localhost:3000"
)

// The adapter is the Engine the handlers consume.
var _ handlers.Engine = (*ceph.Client)(nil)

// The contract runs the adapter against a real gateway started by
// deploy/ceph/bootstrap.sh and checks, with S3 clients holding the keys the
// adapter issued, what only the gateway can answer: what a key may and may
// not do, that a full Storage refuses writes, that a stopped Storage refuses
// everyone and comes back whole, and that a deleted one is gone. It pulls the
// Ceph image, so it runs only when asked to.
func TestCephContract(t *testing.T) {
	if os.Getenv("NODERA_STORAGE_SMOKE") == "" {
		t.Skip("set NODERA_STORAGE_SMOKE=true to run the adapter against a real Ceph")
	}

	endpoint := startCeph(t)
	client, err := ceph.New(ceph.Config{
		AdminEndpoint:  endpoint,
		S3Endpoint:     endpoint,
		PublicEndpoint: endpoint,
		AdminAccessKey: adminAccessKey,
		AdminSecretKey: adminSecretKey,
		Region:         region,
		WebOrigin:      webOrigin,
		RateLimit:      ceph.RateLimit{ReadOps: 6000, WriteOps: 6000, ReadBytes: 6 << 30, WriteBytes: 6 << 30},
		Timeout:        30 * time.Second,
	})
	require.NoError(t, err)
	ctx := context.Background()

	projectA, projectB := uuid.New(), uuid.New()
	accountA, err := client.CreateAccount(ctx, projectA)
	require.NoError(t, err)
	accountB, err := client.CreateAccount(ctx, projectB)
	require.NoError(t, err)

	t.Run("an account is made once per Project", func(t *testing.T) {
		again, err := client.CreateAccount(ctx, projectA)
		require.NoError(t, err)
		assert.Equal(t, accountA, again)
		assert.NotEqual(t, accountA.ID, accountB.ID)
		assert.NotEqual(t, accountA.AccessKey, accountB.AccessKey)
	})

	const alpha, beta, gamma = "contract-alpha", "contract-beta", "contract-gamma"
	const limit = 1 << 20
	require.NoError(t, client.OpenBucket(ctx, accountA, alpha, limit))
	require.NoError(t, client.OpenBucket(ctx, accountA, alpha, limit), "opening what is open holds")
	require.NoError(t, client.OpenBucket(ctx, accountA, beta, limit))
	require.NoError(t, client.OpenBucket(ctx, accountB, gamma, limit))

	writeKey, err := client.CreateAccessKey(ctx, accountA, alpha, uuid.New(), engine.ReadWrite)
	require.NoError(t, err)
	readKeyID := uuid.New()
	readKey, err := client.CreateAccessKey(ctx, accountA, alpha, readKeyID, engine.Read)
	require.NoError(t, err)
	writer := s3With(endpoint, writeKey.AccessKeyID, writeKey.SecretKey)
	reader := s3With(endpoint, readKey.AccessKeyID, readKey.SecretKey)
	anonymous := s3Anonymous(endpoint)

	t.Run("a read key reads and lists but never writes", func(t *testing.T) {
		put(t, writer, alpha, "docs/readme.txt", "hello")
		assert.Equal(t, "hello", get(t, reader, alpha, "docs/readme.txt"))
		_, err := reader.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(alpha)})
		require.NoError(t, err)

		_, err = reader.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(alpha), Key: aws.String("docs/new.txt"), Body: strings.NewReader("x")})
		requireS3Code(t, err, "AccessDenied")
		_, err = reader.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(alpha), Key: aws.String("docs/readme.txt")})
		requireS3Code(t, err, "AccessDenied")
		_, err = reader.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(alpha), Policy: aws.String(publicReadPolicy(alpha))})
		requireS3Code(t, err, "AccessDenied")
	})

	t.Run("a key reaches only its own bucket", func(t *testing.T) {
		_, err := writer.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(beta), Key: aws.String("x"), Body: strings.NewReader("x")})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(beta)})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(gamma), Key: aws.String("x")})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.ListBuckets(ctx, &s3.ListBucketsInput{})
		requireS3Code(t, err, "AccessDenied")
	})

	t.Run("a key neither creates nor deletes a bucket", func(t *testing.T) {
		_, err := writer.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("contract-delta")})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(alpha)})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(beta)})
		requireS3Code(t, err, "AccessDenied")
	})

	t.Run("a full Storage refuses writes until its limit is raised", func(t *testing.T) {
		big := bytes.Repeat([]byte("n"), 700<<10)
		_, err := writer.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(alpha), Key: aws.String("big/one"), Body: bytes.NewReader(big)})
		require.NoError(t, err)
		_, err = writer.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(alpha), Key: aws.String("big/two"), Body: bytes.NewReader(big)})
		requireS3Code(t, err, "QuotaExceeded")

		require.NoError(t, client.SetSizeLimit(ctx, accountA, alpha, 10*limit))
		_, err = writer.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(alpha), Key: aws.String("big/two"), Body: bytes.NewReader(big)})
		require.NoError(t, err)
		_, err = writer.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(alpha), Delete: &types.Delete{
			Objects: []types.ObjectIdentifier{{Key: aws.String("big/one")}, {Key: aws.String("big/two")}}, Quiet: aws.Bool(true),
		}})
		require.NoError(t, err)
	})

	t.Run("listing pages a folder", func(t *testing.T) {
		for _, key := range []string{"docs/a.txt", "docs/b.txt", "img/c.png", "img/d.png", "root-1.txt", "root-2.txt", "root-3.txt"} {
			put(t, writer, alpha, key, key)
		}
		var folders, objects []string
		cursor := ""
		pages := 0
		for {
			page, err := client.ListObjects(ctx, accountA, alpha, "", cursor, 2)
			require.NoError(t, err)
			pages++
			folders = append(folders, page.Folders...)
			for _, o := range page.Objects {
				objects = append(objects, o.Key)
				assert.Equal(t, int64(len(o.Key)), o.Size)
				assert.WithinDuration(t, time.Now(), o.ModifiedAt, time.Minute)
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		assert.Greater(t, pages, 1, "two entries a page need several pages")
		assert.Equal(t, []string{"docs/", "img/"}, folders)
		assert.Equal(t, []string{"root-1.txt", "root-2.txt", "root-3.txt"}, objects)

		inside, err := client.ListObjects(ctx, accountA, alpha, "docs/", "", 200)
		require.NoError(t, err)
		assert.Empty(t, inside.Folders)
		assert.Equal(t, []string{"docs/a.txt", "docs/b.txt", "docs/readme.txt"}, keysOf(inside))
		assert.Empty(t, inside.Next)

		require.NoError(t, client.DeleteObjects(ctx, accountA, alpha, []string{"root-1.txt", "root-2.txt", "root-3.txt", "never-was.txt"}))
		after, err := client.ListObjects(ctx, accountA, alpha, "", "", 200)
		require.NoError(t, err)
		assert.Empty(t, after.Objects)
	})

	t.Run("presigned URLs work from a plain HTTP client", func(t *testing.T) {
		upload, err := client.PresignUpload(ctx, accountA, alpha, "browser/up.txt", "", 5*time.Minute)
		require.NoError(t, err)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, upload, strings.NewReader("from the browser"))
		require.NoError(t, err)
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)

		// A browser sends the file's type; the gateway refuses a type that was
		// not signed and takes the one that was.
		req, err = http.NewRequestWithContext(ctx, http.MethodPut, upload, strings.NewReader("typed"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "text/plain")
		res, err = http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusForbidden, res.StatusCode, "an unsigned type is refused")

		typed, err := client.PresignUpload(ctx, accountA, alpha, "browser/typed.txt", "text/plain", 5*time.Minute)
		require.NoError(t, err)
		req, err = http.NewRequestWithContext(ctx, http.MethodPut, typed, strings.NewReader("typed"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "text/plain")
		res, err = http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode, "the signed type is taken")

		// An upload is private; the browser's switch lets anyone read it and
		// takes that back, and never lets anyone write.
		anonymous := endpoint + "/" + alpha + "/browser/typed.txt"
		res, err = http.Get(anonymous) //nolint:gosec,noctx // a plain fetch, as a visitor's browser would do
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusForbidden, res.StatusCode, "an upload is private")
		public, err := client.ObjectAccess(ctx, accountA, alpha, "browser/typed.txt")
		require.NoError(t, err)
		assert.False(t, public)

		require.NoError(t, client.SetObjectAccess(ctx, accountA, alpha, "browser/typed.txt", true))
		res, err = http.Get(anonymous) //nolint:gosec,noctx // as above
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode, "anyone reads a published object")
		public, err = client.ObjectAccess(ctx, accountA, alpha, "browser/typed.txt")
		require.NoError(t, err)
		assert.True(t, public)
		req, err = http.NewRequestWithContext(ctx, http.MethodPut, anonymous, strings.NewReader("overwritten"))
		require.NoError(t, err)
		res, err = http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusForbidden, res.StatusCode, "nobody writes a published object without a key")

		require.NoError(t, client.SetObjectAccess(ctx, accountA, alpha, "browser/typed.txt", false))
		res, err = http.Get(anonymous) //nolint:gosec,noctx // as above
		require.NoError(t, err)
		_ = res.Body.Close()
		require.Equal(t, http.StatusForbidden, res.StatusCode, "taken back")

		_, err = client.ObjectAccess(ctx, accountA, alpha, "browser/missing.txt")
		require.ErrorIs(t, err, engine.ErrNoSuchObject)

		download, err := client.PresignDownload(ctx, accountA, alpha, "browser/up.txt", 5*time.Minute)
		require.NoError(t, err)
		res, err = http.Get(download)
		require.NoError(t, err)
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		require.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "from the browser", string(body))
		assert.True(t, strings.HasPrefix(download, endpoint+"/"+alpha+"/"), "development addresses buckets by path: %s", download)
	})

	t.Run("the web app's CORS rule is put back beside the customer's", func(t *testing.T) {
		own := types.CORSRule{ID: aws.String("customer"), AllowedOrigins: []string{"https://shop.example"}, AllowedMethods: []string{"GET"}}
		_, err := writer.PutBucketCors(ctx, &s3.PutBucketCorsInput{Bucket: aws.String(alpha), CORSConfiguration: &types.CORSConfiguration{CORSRules: []types.CORSRule{own}}})
		require.NoError(t, err)
		cors, err := writer.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(alpha)})
		require.NoError(t, err)
		require.Len(t, cors.CORSRules, 1, "the customer replaced the web app's rule")

		require.NoError(t, client.AllowBrowser(ctx, accountA, alpha))
		require.NoError(t, client.AllowBrowser(ctx, accountA, alpha), "a second call adds no second rule")
		cors, err = writer.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(alpha)})
		require.NoError(t, err)
		require.Len(t, cors.CORSRules, 2)
		assert.Equal(t, []string{"https://shop.example"}, cors.CORSRules[0].AllowedOrigins)
		assert.Equal(t, []string{webOrigin}, cors.CORSRules[1].AllowedOrigins)
		assert.ElementsMatch(t, []string{"GET", "PUT", "HEAD"}, cors.CORSRules[1].AllowedMethods)

		res, err := preflight(ctx, endpoint+"/"+alpha+"/browser/up.txt", webOrigin, "PUT")
		require.NoError(t, err)
		assert.Equal(t, webOrigin, res.Header.Get("Access-Control-Allow-Origin"), "the gateway answers the browser's preflight")
	})

	t.Run("a stopped Storage refuses everyone and resumes whole", func(t *testing.T) {
		// What the customer published: a policy for one folder, an ACL on one object.
		_, err := writer.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(alpha), Policy: aws.String(publicReadPolicy(alpha))})
		require.NoError(t, err)
		put(t, writer, alpha, "public/page.html", "<h1>hi</h1>")
		put(t, writer, alpha, "private/secret.txt", "shh")
		put(t, writer, alpha, "by-acl.txt", "acl")
		_, err = writer.PutObjectAcl(ctx, &s3.PutObjectAclInput{Bucket: aws.String(alpha), Key: aws.String("by-acl.txt"), ACL: types.ObjectCannedACLPublicRead})
		require.NoError(t, err)
		assert.Equal(t, "<h1>hi</h1>", get(t, anonymous, alpha, "public/page.html"))
		assert.Equal(t, "acl", get(t, anonymous, alpha, "by-acl.txt"))
		_, err = anonymous.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(alpha), Key: aws.String("private/secret.txt")})
		requireS3Code(t, err, "AccessDenied")
		corsBefore, err := writer.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(alpha)})
		require.NoError(t, err)
		presignedBefore, err := client.PresignDownload(ctx, accountA, alpha, "private/secret.txt", 5*time.Minute)
		require.NoError(t, err)

		require.NoError(t, client.Stop(ctx, accountA, alpha))
		require.NoError(t, client.Stop(ctx, accountA, alpha), "stopping what is stopped holds")

		for name, c := range map[string]*s3.Client{"read_write key": writer, "read key": reader, "anonymous": anonymous} {
			for _, key := range []string{"public/page.html", "by-acl.txt", "private/secret.txt"} {
				_, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(alpha), Key: aws.String(key)})
				requireS3Code(t, err, "AccessDenied", "%s reading %s while stopped", name, key)
			}
			_, err := c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(alpha)})
			requireS3Code(t, err, "AccessDenied", "%s listing while stopped", name)
			_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(alpha), Key: aws.String("stopped.txt"), Body: strings.NewReader("x")})
			requireS3Code(t, err, "AccessDenied", "%s writing while stopped", name)
			_, err = c.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(alpha), Key: aws.String("by-acl.txt")})
			requireS3Code(t, err, "AccessDenied", "%s deleting while stopped", name)
		}
		res, err := http.Get(presignedBefore)
		require.NoError(t, err)
		_ = res.Body.Close()
		assert.Equal(t, http.StatusForbidden, res.StatusCode, "a URL presigned before the Stop dies with it")

		// The read_write key may write the bucket's policy when the Storage runs;
		// stopped, it cannot lift the Stop.
		_, err = writer.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String(alpha), Policy: aws.String(publicReadPolicy(alpha))})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(alpha)})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.PutBucketAcl(ctx, &s3.PutBucketAclInput{Bucket: aws.String(alpha), ACL: types.BucketCannedACLPublicRead})
		requireS3Code(t, err, "AccessDenied")
		_, err = writer.PutBucketCors(ctx, &s3.PutBucketCorsInput{Bucket: aws.String(alpha), CORSConfiguration: &types.CORSConfiguration{CORSRules: corsBefore.CORSRules}})
		requireS3Code(t, err, "AccessDenied")
		_, err = anonymous.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(alpha), Key: aws.String("public/page.html")})
		requireS3Code(t, err, "AccessDenied", "still stopped after the key's attempts")

		// A key made during the Stop is as stopped as the others.
		lateKey, err := client.CreateAccessKey(ctx, accountA, alpha, uuid.New(), engine.ReadWrite)
		require.NoError(t, err)
		late := s3With(endpoint, lateKey.AccessKeyID, lateKey.SecretKey)
		_, err = late.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(alpha), Key: aws.String("public/page.html")})
		requireS3Code(t, err, "AccessDenied")

		// The adapter itself still sees the stopped bucket.
		listing, err := client.ListObjects(ctx, accountA, alpha, "", "", 200)
		require.NoError(t, err)
		assert.Len(t, listing.Objects, 1)
		stats, err := client.Stats(ctx, accountA, alpha)
		require.NoError(t, err)
		assert.Greater(t, stats.Objects, int64(3))
		require.NoError(t, client.AllowBrowser(ctx, accountA, alpha), "the CORS rule may be re-applied to a stopped bucket")

		require.NoError(t, client.Resume(ctx, accountA, alpha))
		require.NoError(t, client.Resume(ctx, accountA, alpha), "resuming what runs holds")

		assert.Equal(t, "<h1>hi</h1>", get(t, anonymous, alpha, "public/page.html"), "the customer's policy is back")
		assert.Equal(t, "acl", get(t, anonymous, alpha, "by-acl.txt"), "the object's ACL is back")
		_, err = anonymous.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(alpha), Key: aws.String("private/secret.txt")})
		requireS3Code(t, err, "AccessDenied")
		assert.Equal(t, "shh", get(t, reader, alpha, "private/secret.txt"))
		put(t, writer, alpha, "resumed.txt", "back")
		assert.Equal(t, "back", get(t, late, alpha, "resumed.txt"))
		policy, err := writer.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(alpha)})
		require.NoError(t, err)
		assert.JSONEq(t, publicReadPolicy(alpha), aws.ToString(policy.Policy), "the policy is the customer's, nothing more")
		corsAfter, err := writer.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(alpha)})
		require.NoError(t, err)
		assert.Equal(t, corsBefore.CORSRules, corsAfter.CORSRules)
		res, err = http.Get(presignedBefore)
		require.NoError(t, err)
		_ = res.Body.Close()
		assert.Equal(t, http.StatusOK, res.StatusCode)
	})

	t.Run("a Storage without a policy of its own has none again after a Resume", func(t *testing.T) {
		betaKey, err := client.CreateAccessKey(ctx, accountA, beta, uuid.New(), engine.ReadWrite)
		require.NoError(t, err)
		betaWriter := s3With(endpoint, betaKey.AccessKeyID, betaKey.SecretKey)
		put(t, betaWriter, beta, "one.txt", "1")

		require.NoError(t, client.Stop(ctx, accountA, beta))
		_, err = betaWriter.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(beta), Key: aws.String("one.txt")})
		requireS3Code(t, err, "AccessDenied")
		require.NoError(t, client.Resume(ctx, accountA, beta))
		assert.Equal(t, "1", get(t, betaWriter, beta, "one.txt"))
		_, err = betaWriter.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(beta)})
		requireS3Code(t, err, "NoSuchBucketPolicy")
	})

	t.Run("a Storage is rate limited from its opening", func(t *testing.T) {
		// Five writes a minute, which the opening itself (its CORS rule) already
		// draws on, so a burst of six object writes cannot all pass. The limit
		// is the adapter's configuration, so a second adapter opens this bucket.
		slow, err := ceph.New(ceph.Config{
			AdminEndpoint: endpoint, S3Endpoint: endpoint, PublicEndpoint: endpoint,
			AdminAccessKey: adminAccessKey, AdminSecretKey: adminSecretKey, Region: region, WebOrigin: webOrigin,
			RateLimit: ceph.RateLimit{ReadOps: 5, WriteOps: 5},
		})
		require.NoError(t, err)
		const throttled = "contract-throttled"
		require.NoError(t, slow.OpenBucket(ctx, accountA, throttled, limit))
		key, err := slow.CreateAccessKey(ctx, accountA, throttled, uuid.New(), engine.ReadWrite)
		require.NoError(t, err)
		once := s3With(endpoint, key.AccessKeyID, key.SecretKey, func(o *s3.Options) { o.Retryer = aws.NopRetryer{} })
		written := 0
		var refused error
		for i := 0; i < 6 && refused == nil; i++ {
			_, refused = once.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(throttled), Key: aws.String("1"), Body: strings.NewReader("1")})
			if refused == nil {
				written++
			}
		}
		requireS3Code(t, refused, "SlowDown")
		assert.GreaterOrEqual(t, written, 1)
		assert.Equal(t, "1", get(t, once, throttled, "1"), "reads have their own allowance")

		// The limit counts the adapter's own S3 calls on the bucket too, so a
		// throttled bucket is managed again only once its limit is raised.
		err = slow.DeleteBucket(ctx, accountA, throttled)
		require.ErrorIs(t, err, engine.ErrUnavailable, "%v", err)
		require.NoError(t, client.OpenBucket(ctx, accountA, throttled, limit), "the usual limit, through the admin API, lifts the throttle")
		require.NoError(t, client.DeleteBucket(ctx, accountA, throttled))
	})

	t.Run("a revoked key stops working", func(t *testing.T) {
		require.NoError(t, client.DeleteAccessKey(ctx, accountA, alpha, readKeyID))
		require.NoError(t, client.DeleteAccessKey(ctx, accountA, alpha, readKeyID), "revoking what is revoked holds")
		_, err := reader.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(alpha), Key: aws.String("resumed.txt")})
		requireS3Code(t, err, "InvalidAccessKeyId")
		require.NoError(t, client.DeleteAccessKey(ctx, accountA, beta, uuid.New()), "a key never made is already gone")
	})

	t.Run("statistics and traffic are read back", func(t *testing.T) {
		stats, err := client.Stats(ctx, accountA, alpha)
		require.NoError(t, err)
		listing, err := client.ListObjects(ctx, accountA, alpha, "", "", 1000)
		require.NoError(t, err)
		count, size := countAll(t, client, accountA, alpha, listing)
		assert.Equal(t, count, stats.Objects)
		assert.Equal(t, size, stats.Bytes)

		from := time.Now().UTC().Truncate(time.Hour)
		var traffic []engine.Traffic
		require.Eventually(t, func() bool {
			traffic, err = client.Traffic(ctx, accountA, alpha, from, from.Add(time.Hour))
			return err == nil && len(traffic) == 1 && traffic[0].BytesSent > 0 && traffic[0].BytesReceived > 1400<<10
		}, 90*time.Second, time.Second, "the usage log never showed this hour: %+v %v", traffic, err)
		assert.Equal(t, from, traffic[0].Hour)
		assert.Greater(t, traffic[0].Requests, int64(20))

		empty, err := client.Traffic(ctx, accountA, alpha, from.Add(-48*time.Hour), from.Add(-47*time.Hour))
		require.NoError(t, err)
		assert.Empty(t, empty)
	})

	t.Run("a deleted Storage is gone with its versions and its keys", func(t *testing.T) {
		_, err := writer.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(alpha), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled},
		})
		require.NoError(t, err)
		put(t, writer, alpha, "versioned.txt", "v1")
		put(t, writer, alpha, "versioned.txt", "v2")
		_, err = writer.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(alpha), Key: aws.String("versioned.txt")})
		require.NoError(t, err)
		upload, err := writer.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String(alpha), Key: aws.String("unfinished.bin")})
		require.NoError(t, err)
		_, err = writer.UploadPart(ctx, &s3.UploadPartInput{
			Bucket: aws.String(alpha), Key: aws.String("unfinished.bin"), UploadId: upload.UploadId, PartNumber: aws.Int32(1), Body: strings.NewReader("part"),
		})
		require.NoError(t, err)
		versions, err := writer.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(alpha)})
		require.NoError(t, err)
		require.NotEmpty(t, versions.DeleteMarkers)
		require.Greater(t, len(versions.Versions), 3)

		require.NoError(t, client.DeleteBucket(ctx, accountA, alpha))
		require.NoError(t, client.DeleteBucket(ctx, accountA, alpha), "deleting what is gone holds")

		root := s3With(endpoint, accountA.AccessKey, accountA.SecretKey)
		_, err = root.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(alpha)})
		requireS3Code(t, err, "NotFound")
		_, err = writer.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(alpha), Key: aws.String("resumed.txt")})
		requireS3Code(t, err, "InvalidAccessKeyId")
		users, err := iamWith(endpoint, accountA).ListUsers(ctx, &iam.ListUsersInput{PathPrefix: aws.String("/" + alpha + "/")})
		require.NoError(t, err)
		assert.Empty(t, users.Users)
		_, err = client.Stats(ctx, accountA, alpha)
		require.Error(t, err, "a bucket that is gone is not an empty one")
		assert.False(t, errors.Is(err, engine.ErrUnavailable))

		// The name is free again, for anyone.
		require.NoError(t, client.OpenBucket(ctx, accountB, alpha, limit))
		require.NoError(t, client.DeleteBucket(ctx, accountB, alpha))
	})
}

func startCeph(t *testing.T) string {
	t.Helper()
	script, err := filepath.Abs("../../../../deploy/ceph")
	require.NoError(t, err)

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)
	pool.MaxWait = 5 * time.Minute
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: cephImage, Tag: cephTag,
		Entrypoint: []string{"/nodera/bootstrap.sh"},
		Env: []string{
			"RGW_ADMIN_ACCESS_KEY=" + adminAccessKey,
			"RGW_ADMIN_SECRET_KEY=" + adminSecretKey,
		},
		Mounts:       []string{script + ":/nodera:ro"},
		ExposedPorts: []string{"7480/tcp"},
	}, func(hostConfig *docker.HostConfig) {
		hostConfig.AutoRemove = true
		hostConfig.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Purge(resource) })

	endpoint := "http://127.0.0.1:" + resource.GetPort("7480/tcp")
	started := time.Now()
	require.NoError(t, pool.Retry(func() error {
		res, err := http.Get(endpoint + "/")
		if err != nil {
			return err
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("gateway answered %d", res.StatusCode)
		}

		return nil
	}))
	t.Logf("gateway ready after %s", time.Since(started).Round(time.Second))

	return endpoint
}

func s3With(endpoint, accessKey, secretKey string, options ...func(*s3.Options)) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint), Region: region, UsePathStyle: true,
		Credentials:                credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}, options...)
}

func s3Anonymous(endpoint string) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint), Region: region, UsePathStyle: true, Credentials: aws.AnonymousCredentials{},
	})
}

func iamWith(endpoint string, account *engine.Account) *iam.Client {
	return iam.New(iam.Options{
		BaseEndpoint: aws.String(endpoint), Region: region,
		Credentials: credentials.NewStaticCredentialsProvider(account.AccessKey, account.SecretKey, ""),
	})
}

func put(t *testing.T, client *s3.Client, bucket, key, body string) {
	t.Helper()
	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader(body)})
	require.NoError(t, err, "putting %s/%s", bucket, key)
}

func get(t *testing.T, client *s3.Client, bucket, key string) string {
	t.Helper()
	out, err := client.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	require.NoError(t, err, "getting %s/%s", bucket, key)
	defer func() { _ = out.Body.Close() }()
	body, err := io.ReadAll(out.Body)
	require.NoError(t, err)

	return string(body)
}

func keysOf(listing *engine.Listing) []string {
	keys := make([]string, 0, len(listing.Objects))
	for _, o := range listing.Objects {
		keys = append(keys, o.Key)
	}
	sort.Strings(keys)

	return keys
}

// countAll walks every folder of the bucket and sums what the listing shows.
func countAll(t *testing.T, client *ceph.Client, account *engine.Account, bucket string, listing *engine.Listing) (int64, int64) {
	t.Helper()
	var count, size int64
	for _, o := range listing.Objects {
		count++
		size += o.Size
	}
	for _, folder := range listing.Folders {
		inner, err := client.ListObjects(context.Background(), account, bucket, folder, "", 1000)
		require.NoError(t, err)
		c, s := countAll(t, client, account, bucket, inner)
		count += c
		size += s
	}

	return count, size
}

func publicReadPolicy(bucket string) string {
	doc := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Sid": "customer-public-folder", "Effect": "Allow", "Principal": map[string]any{"AWS": []string{"*"}},
			"Action": []string{"s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/public/*"},
		}},
	}
	raw, _ := json.Marshal(doc)

	return string(raw)
}

func preflight(ctx context.Context, target, origin, method string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, target, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", method)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	_ = res.Body.Close()

	return res, nil
}

// requireS3Code asserts the gateway refused with the given code; the
// optional format and arguments say which step was being tried.
func requireS3Code(t *testing.T, err error, code string, step ...any) {
	t.Helper()
	detail := ""
	if len(step) > 0 {
		detail = fmt.Sprintf(step[0].(string), step[1:]...)
	}
	require.Error(t, err, detail)
	var apiErr smithy.APIError
	require.True(t, errors.As(err, &apiErr), "%s: not an S3 error: %v", detail, err)
	require.Equal(t, code, apiErr.ErrorCode(), "%s: %v", detail, err)
}
