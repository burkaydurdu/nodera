package ceph

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/engine"
)

// keyPolicyName is the one inline policy of an Access key's IAM user.
const keyPolicyName = "storage"

// keyPath is the IAM path every key of a bucket is made under, so the keys of
// a bucket are found from its name alone.
func keyPath(bucket string) string {
	return "/" + bucket + "/"
}

// CreateAccessKey makes the IAM user named after the key's id under the
// bucket's path, gives it the policy of its permission and exactly one access
// key. A user left behind by an earlier failed attempt is finished rather
// than refused: its old key, whose secret nobody holds, is replaced.
func (c *Client) CreateAccessKey(ctx context.Context, account *engine.Account, bucket string, keyID uuid.UUID, permission engine.Permission) (*engine.Credentials, error) {
	policy, err := keyPolicy(bucket, permission)
	if err != nil {
		return nil, err
	}
	client := c.iamFor(account)
	name := keyID.String()
	_, err = client.CreateUser(ctx, &iam.CreateUserInput{UserName: aws.String(name), Path: aws.String(keyPath(bucket))})
	if err != nil && !hasCode(err, "EntityAlreadyExists") {
		return nil, wrap("creating the Access key's user", err)
	}
	_, err = client.PutUserPolicy(ctx, &iam.PutUserPolicyInput{
		UserName: aws.String(name), PolicyName: aws.String(keyPolicyName), PolicyDocument: aws.String(policy),
	})
	if err != nil {
		return nil, wrap("writing the Access key's policy", err)
	}
	if err := c.deleteUserKeys(ctx, client, name); err != nil {
		return nil, err
	}
	created, err := client.CreateAccessKey(ctx, &iam.CreateAccessKeyInput{UserName: aws.String(name)})
	if err != nil {
		return nil, wrap("creating the Access key", err)
	}

	return &engine.Credentials{
		AccessKeyID: aws.ToString(created.AccessKey.AccessKeyId),
		SecretKey:   aws.ToString(created.AccessKey.SecretAccessKey),
	}, nil
}

// DeleteAccessKey removes the key's user with its keys and policy; a user
// already gone is success. A key of another bucket is refused rather than
// deleted, since the caller named the wrong Storage.
func (c *Client) DeleteAccessKey(ctx context.Context, account *engine.Account, bucket string, keyID uuid.UUID) error {
	client := c.iamFor(account)
	name := keyID.String()
	user, err := client.GetUser(ctx, &iam.GetUserInput{UserName: aws.String(name)})
	switch {
	case hasCode(err, "NoSuchEntity"):
		return nil
	case err != nil:
		return wrap("looking up the Access key's user", err)
	case aws.ToString(user.User.Path) != keyPath(bucket):
		return fmt.Errorf("access key %s does not belong to bucket %s", keyID, bucket)
	}

	return c.deleteUser(ctx, client, name)
}

// deleteBucketUsers removes every IAM user made under the bucket's path.
func (c *Client) deleteBucketUsers(ctx context.Context, account *engine.Account, bucket string) error {
	client := c.iamFor(account)
	var marker *string
	for {
		users, err := client.ListUsers(ctx, &iam.ListUsersInput{PathPrefix: aws.String(keyPath(bucket)), Marker: marker})
		if err != nil {
			return wrap("listing the bucket's Access keys", err)
		}
		for _, user := range users.Users {
			if err := c.deleteUser(ctx, client, aws.ToString(user.UserName)); err != nil {
				return err
			}
		}
		if !users.IsTruncated {
			return nil
		}
		marker = users.Marker
	}
}

func (c *Client) deleteUser(ctx context.Context, client *iam.Client, name string) error {
	if err := c.deleteUserKeys(ctx, client, name); err != nil {
		return err
	}
	policies, err := client.ListUserPolicies(ctx, &iam.ListUserPoliciesInput{UserName: aws.String(name)})
	if err != nil && !hasCode(err, "NoSuchEntity") {
		return wrap("listing the Access key's policies", err)
	}
	if policies != nil {
		for _, policy := range policies.PolicyNames {
			_, err := client.DeleteUserPolicy(ctx, &iam.DeleteUserPolicyInput{UserName: aws.String(name), PolicyName: aws.String(policy)})
			if err != nil && !hasCode(err, "NoSuchEntity") {
				return wrap("deleting the Access key's policy", err)
			}
		}
	}
	_, err = client.DeleteUser(ctx, &iam.DeleteUserInput{UserName: aws.String(name)})
	if err != nil && !hasCode(err, "NoSuchEntity") {
		return wrap("deleting the Access key's user", err)
	}

	return nil
}

func (c *Client) deleteUserKeys(ctx context.Context, client *iam.Client, name string) error {
	keys, err := client.ListAccessKeys(ctx, &iam.ListAccessKeysInput{UserName: aws.String(name)})
	if err != nil {
		if hasCode(err, "NoSuchEntity") {
			return nil
		}

		return wrap("listing the Access key's keys", err)
	}
	for _, key := range keys.AccessKeyMetadata {
		_, err := client.DeleteAccessKey(ctx, &iam.DeleteAccessKeyInput{UserName: aws.String(name), AccessKeyId: key.AccessKeyId})
		if err != nil && !hasCode(err, "NoSuchEntity") {
			return wrap("deleting the Access key's key", err)
		}
	}

	return nil
}
