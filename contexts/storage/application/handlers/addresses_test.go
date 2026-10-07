package handlers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/piggystats/nodera/contexts/storage/application/handlers"
)

func TestAddresses_ABucketIsASubdomainOfTheEndpoint(t *testing.T) {
	a := handlers.NewAddresses("https://vno1.storage.example/", false)

	assert.Equal(t, "https://vno1.storage.example", a.Endpoint("vno1"))
	assert.Equal(t, "https://shop-assets.vno1.storage.example", a.BucketURL("vno1", "shop-assets"))
}

func TestAddresses_DevelopmentPutsTheBucketInThePath(t *testing.T) {
	a := handlers.NewAddresses("http://localhost:7480", true)

	assert.Equal(t, "http://localhost:7480/shop-assets", a.BucketURL("vno1", "shop-assets"))
}
