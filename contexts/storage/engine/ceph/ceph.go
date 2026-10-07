// Package ceph is the engine adapter for Ceph's RADOS Gateway (docs/adr/0012):
// a Project is an account, a Storage is one bucket of it under a quota, and an
// Access key is an IAM user of the account whose only policy names that bucket.
// Three APIs of the one gateway are driven: the admin ops API with the admin
// user's key, and the IAM and S3 APIs with the Project account's root key.
package ceph

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/utils/str"
)

// RateLimit is what one Storage may do in a minute, in each direction. A zero
// leaves that direction unlimited. The gateway counts every request on the
// bucket, the storage service's own included, so a Storage whose keys use up
// the minute answers the service SlowDown (engine.ErrUnavailable) until the
// next; the limit is meant to be far above honest use.
type RateLimit struct {
	ReadOps    int64
	WriteOps   int64
	ReadBytes  int64
	WriteBytes int64
}

// Config is one Region's gateway as the storage service reaches it.
type Config struct {
	// AdminEndpoint serves the admin ops API; S3Endpoint the S3 and IAM APIs.
	// They are usually the same gateway and may be the same address.
	AdminEndpoint string
	S3Endpoint    string
	// PublicEndpoint is the address browsers reach, which presigned URLs are
	// signed for. With VirtualHosted the bucket is the first label of its
	// host (<bucket>.<host>); otherwise it is the first path segment, which
	// is what development uses.
	PublicEndpoint string
	VirtualHosted  bool

	AdminAccessKey string
	AdminSecretKey string
	// Region is the SigV4 signing region; the gateway accepts any string as
	// long as every request uses the same one.
	Region string

	// WebOrigin is the web app's origin, allowed to GET, PUT and HEAD a
	// Storage's objects from the browser (presigned uploads and downloads).
	WebOrigin string
	RateLimit RateLimit
	Timeout   time.Duration
}

// Client drives one gateway. It is safe for concurrent use.
type Client struct {
	cfg    Config
	http   *http.Client
	admin  *adminClient
	public *url.URL
}

func New(cfg Config) (*Client, error) {
	for name, value := range map[string]string{
		"admin endpoint": cfg.AdminEndpoint, "s3 endpoint": cfg.S3Endpoint, "public endpoint": cfg.PublicEndpoint,
		"admin access key": cfg.AdminAccessKey, "admin secret key": cfg.AdminSecretKey, "region": cfg.Region,
		"web origin": cfg.WebOrigin,
	} {
		if str.IsBlank(value) {
			return nil, fmt.Errorf("storage engine config: %s is required", name)
		}
	}
	public, err := url.Parse(cfg.PublicEndpoint)
	if err != nil || str.IsEmpty(public.Host) {
		return nil, fmt.Errorf("storage engine config: public endpoint %q is not a URL", cfg.PublicEndpoint)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	httpClient := &http.Client{Timeout: cfg.Timeout}

	return &Client{
		cfg:    cfg,
		http:   httpClient,
		admin:  newAdminClient(strings.TrimRight(cfg.AdminEndpoint, "/"), cfg.AdminAccessKey, cfg.AdminSecretKey, cfg.Region, httpClient),
		public: public,
	}, nil
}

// s3For signs as the Project's root user against the endpoint the service
// reaches; always path-style, which works whatever the gateway's DNS name.
func (c *Client) s3For(account *engine.Account) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint:               aws.String(c.cfg.S3Endpoint),
		Region:                     c.cfg.Region,
		Credentials:                credentials.NewStaticCredentialsProvider(account.AccessKey, account.SecretKey, ""),
		UsePathStyle:               true,
		HTTPClient:                 c.http,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
}

// s3Public signs for the address the browser will use, so the signature
// covers the host the gateway sees.
func (c *Client) s3Public(account *engine.Account) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(c.cfg.PublicEndpoint),
		Region:       c.cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(account.AccessKey, account.SecretKey, ""),
		UsePathStyle: !c.cfg.VirtualHosted,
		HTTPClient:   c.http,
	})
}

func (c *Client) iamFor(account *engine.Account) *iam.Client {
	return iam.New(iam.Options{
		BaseEndpoint: aws.String(c.cfg.S3Endpoint),
		Region:       c.cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(account.AccessKey, account.SecretKey, ""),
		HTTPClient:   c.http,
	})
}

// wrap turns what the gateway or the network answered into the port's terms:
// a gateway that could not be reached, or answered with its own failure, is
// engine.ErrUnavailable; a refusal keeps its code. The engine's name stays out
// of the message because it may be shown to a customer.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if unavailable(err) {
		return fmt.Errorf("%s: %w: %w", op, engine.ErrUnavailable, err)
	}

	return fmt.Errorf("%s: %w", op, err)
}

func unavailable(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var status *awshttp.ResponseError
	if errors.As(err, &status) && status.HTTPStatusCode() >= http.StatusInternalServerError {
		return true
	}
	var admin *adminError
	if errors.As(err, &admin) && admin.status >= http.StatusInternalServerError {
		return true
	}

	return false
}

// hasCode reports whether the gateway refused with the given S3 or IAM error
// code, such as NoSuchBucket or NoSuchEntity.
func hasCode(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, code := range codes {
		if apiErr.ErrorCode() == code {
			return true
		}
	}

	return false
}
