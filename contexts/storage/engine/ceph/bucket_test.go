package ceph

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
)

func webCORS() types.CORSRule {
	return types.CORSRule{ID: aws.String(corsRuleID), AllowedOrigins: []string{"https://app.nodera.test"}, AllowedMethods: []string{"GET", "PUT", "HEAD"}}
}

func TestMergeCORSAppendsTheWebRuleAfterTheCustomers(t *testing.T) {
	own := types.CORSRule{ID: aws.String("shop"), AllowedOrigins: []string{"https://shop.example"}, AllowedMethods: []string{"GET"}}

	merged := mergeCORS([]types.CORSRule{own}, webCORS())

	assert.Equal(t, []types.CORSRule{own, webCORS()}, merged)
}

func TestMergeCORSReplacesAnEarlierWebRuleInPlace(t *testing.T) {
	stale := types.CORSRule{ID: aws.String(corsRuleID), AllowedOrigins: []string{"https://old.nodera.test"}, AllowedMethods: []string{"GET"}}
	own := types.CORSRule{ID: aws.String("shop"), AllowedOrigins: []string{"https://shop.example"}, AllowedMethods: []string{"GET"}}

	merged := mergeCORS([]types.CORSRule{stale, own}, webCORS())

	assert.Equal(t, []types.CORSRule{webCORS(), own}, merged)
}

func TestMergeCORSKeepsOneWebRuleWhenSeveralWereLeft(t *testing.T) {
	merged := mergeCORS([]types.CORSRule{webCORS(), webCORS()}, webCORS())

	assert.Equal(t, []types.CORSRule{webCORS()}, merged)
}

func TestMergeCORSStartsFromNothing(t *testing.T) {
	assert.Equal(t, []types.CORSRule{webCORS()}, mergeCORS(nil, webCORS()))
}
