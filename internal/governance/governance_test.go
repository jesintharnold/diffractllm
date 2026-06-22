package governance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// ------------ VIRTUAL KEYS ----------

func TestValidateKeySignature(t *testing.T) {
	apiKey, _, _, err := GenerateVirtualKey()
	assert.NoError(t, err, "Generating a key should not produce an error")

	tests := []struct {
		name     string
		apiKey   string
		expected bool
	}{
		{
			name:     "Valid key",
			apiKey:   apiKey,
			expected: true,
		},
		{
			name:     "Invalid Length",
			apiKey:   "rk-waytooshort",
			expected: false,
		}, {
			name:     "Missing Prefix",
			apiKey:   "ab-1234567890123456789012",
			expected: false,
		},
		{
			name:     "Invalid Base62 Character",
			apiKey:   "rk-!@#$%^&*()_+1234567890",
			expected: false,
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			actual := ValidateKeySignature(testcase.apiKey)
			assert.Equal(t, testcase.expected, actual, "Failed test case: %s", testcase.name)
		})
	}

}

func TestVirtualKeyCache_Lookup(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Returns nil when map is nil", func(t *testing.T) {
		cache := &VirtualkeyCache{
			logger: logger,
		}

		entry, ok := cache.Lookup("test-key")

		assert.False(t, ok, "Should return false if the map pointer is nil")
		assert.Nil(t, entry, "Entry should be nil if the map pointer is nil")
	})

	t.Run("Returns entry when key exists", func(t *testing.T) {
		vkcache := &VirtualkeyCache{
			logger: logger,
		}

		expectedVirtualKey := &VirtualKey{
			Key:      "rk-test12345",
			ClientID: "client-alpha",
		}

		vkcache.Upsert(expectedVirtualKey.Key, expectedVirtualKey)
		entry, exists := vkcache.Lookup("rk-test12345")
		assert.True(t, exists, "Should return true because the key is in the map")
		assert.Equal(t, expectedVirtualKey, entry, "Should return the exact VirtualKey we stored")

	})

	t.Run("Returns false when key does not exist", func(t *testing.T) {
		vkcache := &VirtualkeyCache{
			logger: logger,
		}

		expectedVirtualKey := &VirtualKey{
			Key:      "rk-test12345",
			ClientID: "client-alpha",
		}

		vkcache.Upsert(expectedVirtualKey.Key, expectedVirtualKey)
		entry, exists := vkcache.Lookup("rk-test12")

		assert.False(t, exists, "Should return false because this key isn't in the map")
		assert.Nil(t, entry, "Entry should be nil for a missing key")

	})

}

func TestVirtualKeyCache_Upsert_Delete(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Upsert updates an existing key with new values", func(t *testing.T) {
		vkcache := &VirtualkeyCache{logger: logger}

		expectedVirtualKey := &VirtualKey{Key: "rk-test12345", ClientID: "client-alpha"}
		duplicateVirtualkey := &VirtualKey{Key: "rk-test12345", ClientID: "client-beta"}

		vkcache.Upsert(expectedVirtualKey.Key, expectedVirtualKey)
		vkcache.Upsert(duplicateVirtualkey.Key, duplicateVirtualkey)
		entry, exists := vkcache.Lookup("rk-test12345")
		assert.True(t, exists, "Should return true because the key is in the map")
		assert.NotEqual(t, expectedVirtualKey, entry, "Should not equal the old values")
		assert.Equal(t, duplicateVirtualkey, entry, "Should equal the newly updated values")
	})

	t.Run("Delete removes key and returns error if not found", func(t *testing.T) {
		vkcache := &VirtualkeyCache{logger: logger}
		keyToDelete := &VirtualKey{Key: "rk-test12345", ClientID: "client-alpha"}
		vkcache.Upsert(keyToDelete.Key, keyToDelete)

		err := vkcache.Delete("rk-test12345")
		assert.NoError(t, err, "Deleting an existing key shouldn't produce an error")

		err = vkcache.Delete("rk-test12345")
		assert.ErrorContains(t, err, "virtual key not found")
	})

	t.Run("Delete returns error if cache is totally empty (nil map)", func(t *testing.T) {
		vkcache := &VirtualkeyCache{logger: logger}

		err := vkcache.Delete("rk-any-key")
		assert.ErrorContains(t, err, "virtual key store not found")
	})
}

// ------ USAGE BUFFER -------

func TestUsageBuffer_AppendAndDrain(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Successfully appends and drains records", func(t *testing.T) {
		ub := NewUsageBuffer(5, logger)
		record1 := UsageRecord{ClientID: "client-1", Cost: 100}
		record2 := UsageRecord{ClientID: "client-2", Cost: 200}
		ub.Append(record1)
		ub.Append(record2)
		assert.Equal(t, int64(0), ub.DroppedCount(), "No records should be dropped")
		drained := ub.Drain()
		assert.Len(t, drained, 2, "Should drain exactly 2 records")
		assert.Equal(t, "client-1", drained[0].ClientID)
		assert.Equal(t, "client-2", drained[1].ClientID)
		drainAgain := ub.Drain()
		assert.Len(t, drainAgain, 0, "Buffer should be empty after the first drain")
	})

	t.Run("Drops records when max capacity is reached", func(t *testing.T) {
		ub := NewUsageBuffer(2, logger)
		record1 := UsageRecord{ClientID: "client-1", Cost: 100}
		record2 := UsageRecord{ClientID: "client-2", Cost: 200}
		record3 := UsageRecord{ClientID: "client-3", Cost: 300}
		record4 := UsageRecord{ClientID: "client-4", Cost: 400}

		ub.Append(record1)
		ub.Append(record2)
		ub.Append(record3)
		ub.Append(record4)

		assert.Equal(t, int64(2), ub.DroppedCount(), "Exactly 2 records should be dropped")

		drained := ub.Drain()
		assert.Len(t, drained, 2, "Should drain exactly 2 records")
		assert.Equal(t, "client-1", drained[0].ClientID)
		assert.Equal(t, "client-2", drained[1].ClientID)
	})

}

// -------- BUDGET ---------

func TestBudget_IsOverBudget(t *testing.T) {
	t.Run("Returns false if EnforceBudget is turned off", func(t *testing.T) {
		b := &Budget{
			EnforceBudget: false,
			BudgetLimit:   100,
		}
		b.TotalCost.Store(5000)

		assert.False(t, b.IsOverBudget(), "Should not be over budget if enforcement is false")
	})

	t.Run("Returns false if BudgetLimit is 0 (Unlimited)", func(t *testing.T) {
		b := &Budget{
			EnforceBudget: true,
			BudgetLimit:   0,
		}
		b.TotalCost.Store(5000)

		assert.False(t, b.IsOverBudget(), "Should not be over budget if limit is 0")
	})

	t.Run("Returns true when TotalCost hits or exceeds limit", func(t *testing.T) {
		b := &Budget{
			EnforceBudget: true,
			BudgetLimit:   100,
		}
		b.TotalCost.Store(100)

		assert.True(t, b.IsOverBudget(), "Should return true when cost reaches limit")

		b.TotalCost.Store(150)
		assert.True(t, b.IsOverBudget(), "Should return true when cost exceeds limit")
	})
}

func TestBudget_RecordUsageAndReset(t *testing.T) {
	t.Run("Successfully records usage", func(t *testing.T) {
		b := &Budget{}
		b.RecordUsage(500)
		assert.Equal(t, int64(500), b.TotalCost.Load())
		assert.Equal(t, int64(500), b.PendingCost.Load())
		assert.Equal(t, int64(1), b.RequestCount.Load())
		assert.Equal(t, int64(1), b.PendingRequests.Load())
	})

	t.Run("Time Travel: Resets counters if budget window expired", func(t *testing.T) {
		b := &Budget{
			BudgetDuration:    60,
			NextBudgetResetAt: time.Now().Add(-1 * time.Minute), // Just a small trick to think it is expired
		}

		b.TotalCost.Store(1000)
		b.PendingCost.Store(1000)
		b.RequestCount.Store(5)
		b.PendingRequests.Store(5)

		b.RecordUsage(50)

		assert.Equal(t, int64(50), b.TotalCost.Load(), "Old cost should be wiped, only new cost remains")
		assert.Equal(t, int64(50), b.PendingCost.Load(), "Old pending cost should be wiped")
		assert.Equal(t, int64(1), b.RequestCount.Load(), "Request count should reset to 1")
		assert.True(t, b.NextBudgetResetAt.After(time.Now()), "Next reset time should be in the future now")
	})
}
