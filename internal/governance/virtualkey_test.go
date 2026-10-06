package governance

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"diffractllm/internal/core"
)

// ---------- fixtures owned by this file ----------

func vkFixture(id, key string, opts ...func(*core.VirtualKey)) *core.VirtualKey {
	compiled, err := core.CompileProviderConfigs(core.VKDirect, []core.ProviderConfig{
		{Provider: core.ProviderOpenAI, AllowedModels: []string{"*"}},
	})
	if err != nil {
		panic(err)
	}
	vk := &core.VirtualKey{
		ID: id, Key: key, ClientID: "client-1", BudgetID: "budget-1",
		IsActive: true, Mode: core.VKDirect, LoadBalancer: core.LBRoundRobin,
		ProviderConfigs: compiled,
	}
	for _, opt := range opts {
		opt(vk)
	}
	return vk
}

func vkCache(t *testing.T, keys ...*core.VirtualKey) *VirtualkeyCache {
	t.Helper()
	kc := &VirtualkeyCache{logger: zap.NewNop()}
	kc.LoadVirtualKeys(keys)
	return kc
}
func TestLookupOnAnUnloadedCache(t *testing.T) {
	kc := &VirtualkeyCache{logger: zap.NewNop()}

	vk, found := kc.LookupVkey("dk-anything")
	assert.Nil(t, vk)
	assert.False(t, found)
	assert.True(t, kc.LastSync.IsZero())
}

// The map is keyed by the plaintext key because that is what arrives on a
// request header. The id is not a lookup path.
func TestLookupIsByPlaintextKeyNotID(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"))

	vk, found := kc.LookupVkey("dk-aaa")
	require.True(t, found)
	assert.Equal(t, "id-1", vk.ID)

	_, found = kc.LookupVkey("id-1")
	assert.False(t, found)

	_, found = kc.LookupVkey("")
	assert.False(t, found)
}

func TestLoadVirtualKeysReplacesWholesale(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"), vkFixture("id-2", "dk-bbb"))
	first := kc.LastSync
	require.False(t, first.IsZero())

	kc.LoadVirtualKeys([]*core.VirtualKey{vkFixture("id-3", "dk-ccc")})

	_, found := kc.LookupVkey("dk-ccc")
	assert.True(t, found)
	_, found = kc.LookupVkey("dk-aaa")
	assert.False(t, found, "a load is a replacement, not a merge")
	_, found = kc.LookupVkey("dk-bbb")
	assert.False(t, found)

	assert.False(t, kc.LastSync.Before(first), "LastSync must move forward")
}

func TestLoadVirtualKeysWithNothingEmptiesTheCache(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"))

	kc.LoadVirtualKeys(nil)

	_, found := kc.LookupVkey("dk-aaa")
	assert.False(t, found)
	assert.NotNil(t, kc.virtual.Load(), "an empty load still publishes a snapshot")
}

// Two rows sharing a key would collide. The last one wins, which is worth
// knowing because the store's unique index is what prevents it.
func TestLoadVirtualKeysDuplicateKeyLastWins(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-same"), vkFixture("id-2", "dk-same"))

	vk, found := kc.LookupVkey("dk-same")
	require.True(t, found)
	assert.Equal(t, "id-2", vk.ID)
}

func TestUpsertVirtualKey(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"))

	kc.UpsertVirtualKey(vkFixture("id-2", "dk-bbb"))

	_, found := kc.LookupVkey("dk-aaa")
	assert.True(t, found, "an upsert must not disturb other entries")
	_, found = kc.LookupVkey("dk-bbb")
	assert.True(t, found)
}

func TestUpsertVirtualKeyNilIsANoOp(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"))

	kc.UpsertVirtualKey(nil)

	_, found := kc.LookupVkey("dk-aaa")
	assert.True(t, found)
}

// Rotating a key mints a new plaintext, so an upsert under the new key leaves
// the old one resolvable until the next full sync evicts it.
func TestUpsertVirtualKeyWithANewPlaintextLeavesTheOldEntry(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-old"))

	kc.UpsertVirtualKey(vkFixture("id-1", "dk-new"))

	_, found := kc.LookupVkey("dk-new")
	assert.True(t, found)
	_, found = kc.LookupVkey("dk-old")
	assert.True(t, found, "the upsert is keyed by plaintext, so the old key survives")
}

func TestUpsertVirtualKeyOnAnUnloadedCache(t *testing.T) {
	kc := &VirtualkeyCache{logger: zap.NewNop()}

	kc.UpsertVirtualKey(vkFixture("id-1", "dk-aaa"))

	_, found := kc.LookupVkey("dk-aaa")
	assert.True(t, found, "clone must handle a nil snapshot")
}

// Readers hold the map lock-free through an atomic pointer, so a writer has to
// publish a new map rather than mutate the one already handed out.
func TestUpsertDoesNotMutateThePublishedMap(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"))
	held := kc.virtual.Load()
	require.Len(t, *held, 1)

	kc.UpsertVirtualKey(vkFixture("id-2", "dk-bbb"))

	assert.Len(t, *held, 1, "the map a reader already held must not change")
	assert.NotSame(t, held, kc.virtual.Load(), "a new snapshot must be published")
	assert.Len(t, *kc.virtual.Load(), 2)
}

func TestDeleteVirtualKeyByID(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"), vkFixture("id-2", "dk-bbb"))

	assert.True(t, kc.DeleteVirtualKeyByID("id-1"))

	_, found := kc.LookupVkey("dk-aaa")
	assert.False(t, found)
	_, found = kc.LookupVkey("dk-bbb")
	assert.True(t, found)
}

func TestDeleteVirtualKeyByIDMiss(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"))

	assert.False(t, kc.DeleteVirtualKeyByID("id-nope"))

	_, found := kc.LookupVkey("dk-aaa")
	assert.True(t, found, "a failed delete must change nothing")
}

func TestDeleteVirtualKeyByIDOnAnUnloadedCache(t *testing.T) {
	kc := &VirtualkeyCache{logger: zap.NewNop()}

	assert.False(t, kc.DeleteVirtualKeyByID("id-1"))
}

func TestDeleteDoesNotMutateThePublishedMap(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"), vkFixture("id-2", "dk-bbb"))
	held := kc.virtual.Load()
	require.Len(t, *held, 2)

	require.True(t, kc.DeleteVirtualKeyByID("id-1"))

	assert.Len(t, *held, 2, "the map a reader already held must not change")
	assert.Len(t, *kc.virtual.Load(), 1)
}

// A miss publishes nothing, so the snapshot pointer is untouched. That matters
// because a revoke loop over unknown ids would otherwise churn the map.
func TestDeleteMissPublishesNothing(t *testing.T) {
	kc := vkCache(t, vkFixture("id-1", "dk-aaa"))
	before := kc.virtual.Load()

	require.False(t, kc.DeleteVirtualKeyByID("id-nope"))

	assert.Same(t, before, kc.virtual.Load())
}

func TestVirtualKeyCacheConcurrentReadWrite(t *testing.T) {
	kc := vkCache(t, vkFixture("id-0", "dk-000"))

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := range 200 {
			kc.UpsertVirtualKey(vkFixture("id-w", "dk-w"+time.Duration(i).String()))
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			kc.DeleteVirtualKeyByID("id-w")
		}
	}()
	go func() {
		defer wg.Done()
		for range 400 {
			_, _ = kc.LookupVkey("dk-000")
		}
	}()
	wg.Wait()

	_, found := kc.LookupVkey("dk-000")
	assert.True(t, found, "the seeded key must survive the churn")
}
