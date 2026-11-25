package integration

import "testing"

// The real scenarios live behind the integration build tag so `go test ./...`
// works without a database.
func TestIntegrationRequiresBuildTag(t *testing.T) {
	t.Skip("run integration scenarios with: go test -tags integration ./test")
}
