package store

import "testing"

func TestMemory(t *testing.T) {
	runStoreSuite(t, func(*testing.T) storeUnderTest { return NewMemory() })
}
