package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/piggystats/nodera/utils/str"
)

// adminClient speaks the gateway's admin ops API: plain HTTP under /admin,
// SigV4-signed like S3 with the admin user's key. It is written here rather
// than taken from a library because the calls the adapter needs for rate
// limits sit behind a preview build tag there, and because the HTTP status is
// what tells a refusal from a gateway that is down.
type adminClient struct {
	endpoint string
	creds    *credentials.StaticCredentialsProvider
	region   string
	http     *http.Client
	signer   *v4.Signer
}

// adminError is a non-2xx answer of the admin API. The code is the gateway's
// (NoSuchBucket, NoSuchUser, AccountAlreadyExists ...).
type adminError struct {
	status int
	code   string
	body   string
}

func (e *adminError) Error() string {
	if str.IsNotEmpty(e.code) {
		return fmt.Sprintf("storage engine answered %d %s", e.status, e.code)
	}

	return fmt.Sprintf("storage engine answered %d: %s", e.status, e.body)
}

func newAdminClient(endpoint, accessKey, secretKey, region string, httpClient *http.Client) *adminClient {
	creds := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")

	return &adminClient{endpoint: endpoint, creds: &creds, region: region, http: httpClient, signer: v4.NewSigner()}
}

// call sends one admin request. path is the resource ("/user", "/bucket?quota");
// the arguments go in the query, as the API takes them. A JSON answer is
// decoded into out when out is given.
func (a *adminClient) call(ctx context.Context, method, path string, args url.Values, out any) error {
	target := a.endpoint + "/admin" + path
	if len(args) > 0 {
		if strings.ContainsRune(path, '?') {
			target += "&" + args.Encode()
		} else {
			target += "?" + args.Encode()
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, target, http.NoBody)
	if err != nil {
		return err
	}
	creds, err := a.creds.Retrieve(ctx)
	if err != nil {
		return err
	}
	// The admin API takes no body. The gateway assumes an unsigned payload
	// when the header is absent, so the hash signed must be announced.
	const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	req.Header.Set("X-Amz-Content-Sha256", emptyPayloadHash)
	if err := a.signer.SignHTTP(ctx, creds, req, emptyPayloadHash, "s3", a.region, time.Now()); err != nil {
		return err
	}
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var answer struct {
			Code string `json:"Code"`
		}
		_ = json.Unmarshal(body, &answer)

		return &adminError{status: res.StatusCode, code: answer.Code, body: string(body)}
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decoding the admin answer: %w", err)
		}
	}

	return nil
}

// adminConflict reports whether the admin API answered that what was being
// made already exists.
func adminConflict(err error) bool {
	var e *adminError
	if !errors.As(err, &e) {
		return false
	}

	return e.status == http.StatusConflict
}

// adminNotFound reports whether the admin API answered that the account, user
// or bucket asked for does not exist.
func adminNotFound(err error) bool {
	var e *adminError
	if !errors.As(err, &e) {
		return false
	}

	return e.status == http.StatusNotFound
}

type adminAccount struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MaxBuckets int64  `json:"max_buckets"`
}

type adminUser struct {
	ID          string `json:"user_id"`
	DisplayName string `json:"display_name"`
	AccountID   string `json:"account_id"`
	Keys        []struct {
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
	} `json:"keys"`
}

type adminBucket struct {
	Bucket string `json:"bucket"`
	Owner  string `json:"owner"`
	// size is what the objects add up to, which is what the quota counts;
	// size_actual would be the disk they take, rounded up per object.
	Usage map[string]struct {
		Size       int64 `json:"size"`
		NumObjects int64 `json:"num_objects"`
	} `json:"usage"`
}

func (a *adminClient) getAccountByName(ctx context.Context, name string) (*adminAccount, error) {
	var account adminAccount
	if err := a.call(ctx, http.MethodGet, "/account", url.Values{"name": {name}}, &account); err != nil {
		return nil, err
	}

	return &account, nil
}

func (a *adminClient) createAccount(ctx context.Context, name string) (*adminAccount, error) {
	var account adminAccount
	if err := a.call(ctx, http.MethodPost, "/account", url.Values{"name": {name}}, &account); err != nil {
		return nil, err
	}

	return &account, nil
}

func (a *adminClient) getUser(ctx context.Context, uid string) (*adminUser, error) {
	var user adminUser
	if err := a.call(ctx, http.MethodGet, "/user", url.Values{"uid": {uid}}, &user); err != nil {
		return nil, err
	}

	return &user, nil
}

func (a *adminClient) createAccountRoot(ctx context.Context, uid, displayName, accountID string) (*adminUser, error) {
	var user adminUser
	err := a.call(ctx, http.MethodPut, "/user", url.Values{
		"uid": {uid}, "display-name": {displayName}, "account-id": {accountID},
		"account-root": {"true"}, "generate-key": {"true"},
	}, &user)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (a *adminClient) getBucket(ctx context.Context, bucket string) (*adminBucket, error) {
	var info adminBucket
	if err := a.call(ctx, http.MethodGet, "/bucket", url.Values{"bucket": {bucket}, "stats": {"true"}}, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// setBucketQuota caps the bucket at maxSize bytes. The uid is any user the
// gateway can load; ownership is not checked for a bucket named outright.
func (a *adminClient) setBucketQuota(ctx context.Context, uid, bucket string, maxSize int64) error {
	return a.call(ctx, http.MethodPut, "/bucket?quota", url.Values{
		"uid": {uid}, "bucket": {bucket}, "enabled": {"true"}, "max-size": {strconv.FormatInt(maxSize, 10)},
	}, nil)
}

func (a *adminClient) setBucketRateLimit(ctx context.Context, bucket string, limit RateLimit) error {
	return a.call(ctx, http.MethodPost, "/ratelimit?ratelimit-scope=bucket", url.Values{
		"bucket":          {bucket},
		"enabled":         {"true"},
		"max-read-ops":    {strconv.FormatInt(limit.ReadOps, 10)},
		"max-write-ops":   {strconv.FormatInt(limit.WriteOps, 10)},
		"max-read-bytes":  {strconv.FormatInt(limit.ReadBytes, 10)},
		"max-write-bytes": {strconv.FormatInt(limit.WriteBytes, 10)},
	}, nil)
}

type adminUsage struct {
	Entries []struct {
		User    string `json:"user"`
		Buckets []struct {
			Bucket     string `json:"bucket"`
			Epoch      int64  `json:"epoch"`
			Categories []struct {
				Category      string `json:"category"`
				BytesSent     int64  `json:"bytes_sent"`
				BytesReceived int64  `json:"bytes_received"`
				Ops           int64  `json:"ops"`
			} `json:"categories"`
		} `json:"buckets"`
	} `json:"entries"`
}

// accountUsage reads the usage log of everything the account owns. The log
// keys its entries by owner, and in Ceph 20.2.4 a read scoped to one bucket is
// refused (ENOTSUP) when the owner is an account, so the caller picks its
// bucket out of the account's entries.
func (a *adminClient) accountUsage(ctx context.Context, accountID string, from, to time.Time) (*adminUsage, error) {
	// The gateway reads the bounds as dates, not as epochs; an epoch answers
	// nothing at all.
	const layout = "2006-01-02 15:04:05"
	var usage adminUsage
	err := a.call(ctx, http.MethodGet, "/usage", url.Values{
		"uid":          {accountID},
		"start":        {from.UTC().Format(layout)},
		"end":          {to.UTC().Format(layout)},
		"show-entries": {"true"},
		"show-summary": {"false"},
	}, &usage)
	if err != nil {
		return nil, err
	}

	return &usage, nil
}
