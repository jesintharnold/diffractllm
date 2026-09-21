package dbstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"diffractllm/internal/core"
)

// ---------- shared across the dbstore test files ----------
//
// NewStore is a sync.Once singleton, so one database serves the whole test
// binary. Each caller gets it empty apart from the seeded provider rows.

const testAESKey = "0123456789abcdef0123456789abcdef"

func store(t *testing.T) *Store {
	t.Helper()

	path := filepath.Join(os.TempDir(), "diffractllm_dbstore_test.db")
	s, err := NewStore(path, testAESKey, zap.NewNop())
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	require.NoError(t, s.Seed(false))

	for _, table := range []string{
		"usage_records", "model_pricing_override", "model_pricing",
		"model_metadata", "virtual_keys", "credentials", "budgets",
	} {
		require.NoError(t, s.DB.Exec("DELETE FROM "+table).Error)
	}
	return s
}

func tptr[T any](v T) *T { return &v }

// Reads a column with plain SQL, bypassing gorm so no AfterFind hook runs. This
// is the only way to see what is actually on disk.
func rawColumn(t *testing.T, s *Store, table, column, id string) string {
	t.Helper()
	var value string
	row := s.DB.Raw("SELECT "+column+" FROM "+table+" WHERE id = ?", id).Row()
	require.NoError(t, row.Scan(&value))
	return value
}

func newBudgetRow(t *testing.T, s *Store, limit int64) *StoreBudget {
	t.Helper()
	row, err := s.CreateBudget(core.Budget{
		Name:        "budget-" + time.Now().Format("150405.000000000"),
		BudgetLimit: limit, BudgetDuration: "1D",
	})
	require.NoError(t, err)
	return row
}

// ---------- CreateBudget defaults ----------

func TestCreateBudgetDefaults(t *testing.T) {
	s := store(t)
	before := time.Now().UTC().Add(-time.Second)

	row, err := s.CreateBudget(core.Budget{
		Name: "defaults", BudgetLimit: 5_000, BudgetDuration: "1D",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, row.ID, "an id is minted")
	assert.True(t, row.Enforce, "enforce defaults to true, so a budget enforces unless told not to")
	assert.Equal(t, "nanodollars", row.BudgetUnit)
	assert.False(t, row.LastBudgetRefreshAt.IsZero())
	assert.True(t, row.LastBudgetRefreshAt.After(before), "the window starts now")
	assert.Zero(t, row.TotalCost)
	assert.Zero(t, row.RequestCount)
	// BudgetParseDuration is filled by AfterFind, so the row returned straight
	// from Create is not hydrated. A read has it.
	assert.Zero(t, row.BudgetParseDuration)
	got, err := s.GetBudget(row.ID)
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, got.BudgetParseDuration)
}

func TestCreateBudgetHonoursAnExplicitEnforceFalse(t *testing.T) {
	s := store(t)

	row, err := s.CreateBudget(core.Budget{
		Name: "soft", BudgetLimit: 5_000, BudgetDuration: "1D", Enforce: tptr(false),
	})
	require.NoError(t, err)
	assert.False(t, row.Enforce)

	got, err := s.GetBudget(row.ID)
	require.NoError(t, err)
	assert.False(t, got.Enforce, "it survives the round trip")
}

func TestCreateBudgetKeepsAnExplicitRefreshTime(t *testing.T) {
	s := store(t)
	when := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)

	row, err := s.CreateBudget(core.Budget{
		Name: "mid-window", BudgetLimit: 5_000, BudgetDuration: "1D",
		LastBudgetRefreshAt: when,
	})
	require.NoError(t, err)
	assert.Equal(t, when, row.LastBudgetRefreshAt.UTC(),
		"a caller resuming a window must keep its start")
}

func TestCreateBudgetRejections(t *testing.T) {
	tests := []struct {
		name    string
		give    core.Budget
		wantErr string
	}{
		{
			name:    "zero limit",
			give:    core.Budget{Name: "zero", BudgetLimit: 0, BudgetDuration: "1D"},
			wantErr: "budget_limit",
		},
		{
			name:    "negative limit",
			give:    core.Budget{Name: "negative", BudgetLimit: -1, BudgetDuration: "1D"},
			wantErr: "budget_limit",
		},
		{
			name:    "missing duration",
			give:    core.Budget{Name: "no-duration", BudgetLimit: 100},
			wantErr: "duration",
		},
		{
			name:    "unparseable duration",
			give:    core.Budget{Name: "bad-duration", BudgetLimit: 100, BudgetDuration: "1 fortnight"},
			wantErr: "duration",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := store(t)
			_, err := s.CreateBudget(tc.give)
			require.Error(t, err)
			assert.Contains(t, strings.ToLower(err.Error()), tc.wantErr)
		})
	}
}

func TestCreateBudgetRejectsADuplicateName(t *testing.T) {
	s := store(t)

	_, err := s.CreateBudget(core.Budget{Name: "same", BudgetLimit: 100, BudgetDuration: "1D"})
	require.NoError(t, err)

	_, err = s.CreateBudget(core.Budget{Name: "same", BudgetLimit: 200, BudgetDuration: "1D"})
	assert.Error(t, err, "the name carries a unique index")
}

// ---------- ListBudgets and ToCore ----------

// ListBudgets is the governance sync's source, so the parsed duration and the
// Enforce pointer have to survive into core.Budget.
func TestListBudgetsToCore(t *testing.T) {
	s := store(t)
	row := newBudgetRow(t, s, 7_500)

	rows, err := s.ListBudgets()
	require.NoError(t, err)
	require.Len(t, rows, 1)

	got := rows[0].ToCore()
	assert.Equal(t, row.ID, got.ID)
	assert.EqualValues(t, 7_500, got.BudgetLimit)
	assert.Equal(t, 24*time.Hour, got.BudgetParseDuration)
	require.NotNil(t, got.Enforce)
	assert.True(t, *got.Enforce)
}

func TestGetBudgetNotFound(t *testing.T) {
	_, err := store(t).GetBudget("no-such-budget")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------- UpdateBudget ----------

func TestUpdateBudget(t *testing.T) {
	s := store(t)
	row := newBudgetRow(t, s, 1_000)

	updated, err := s.UpdateBudget(row.ID, core.Budget{
		Name: "renamed", BudgetLimit: 9_000, BudgetDuration: "1W", Enforce: tptr(false),
	})
	require.NoError(t, err)

	// Name is deliberately not in the update map, so a rename is a silent no-op.
	assert.Equal(t, row.Name, updated.Name, "UpdateBudget does not rename")
	assert.EqualValues(t, 9_000, updated.BudgetLimit)
	assert.False(t, updated.Enforce)
	assert.Equal(t, 7*24*time.Hour, updated.BudgetParseDuration, "the new duration is reparsed")
}

func TestUpdateBudgetRejectsABadDuration(t *testing.T) {
	s := store(t)
	row := newBudgetRow(t, s, 1_000)

	_, err := s.UpdateBudget(row.ID, core.Budget{
		Name: "bad", BudgetLimit: 1_000, BudgetDuration: "whenever",
	})
	require.Error(t, err)

	unchanged, err := s.GetBudget(row.ID)
	require.NoError(t, err)
	assert.Equal(t, row.Name, unchanged.Name, "a rejected update must change nothing")
}

// ---------- FlushBudgetUsage and ResetBudgetWindow ----------

// The flush writes absolute totals, not deltas, because the cache is the source
// of truth for the live window.
func TestFlushBudgetUsage(t *testing.T) {
	s := store(t)
	row := newBudgetRow(t, s, 1_000_000)

	require.NoError(t, s.FlushBudgetUsage(row.ID, 4_500, 3))

	got, err := s.GetBudget(row.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 4_500, got.TotalCost)
	assert.EqualValues(t, 3, got.RequestCount)
	assert.False(t, got.LastFlushedAt.IsZero())

	require.NoError(t, s.FlushBudgetUsage(row.ID, 6_000, 5))
	got, err = s.GetBudget(row.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 6_000, got.TotalCost, "the second flush overwrites rather than adding")
	assert.EqualValues(t, 5, got.RequestCount)
}

func TestResetBudgetWindow(t *testing.T) {
	s := store(t)
	row := newBudgetRow(t, s, 1_000_000)
	require.NoError(t, s.FlushBudgetUsage(row.ID, 4_500, 3))

	target := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.ResetBudgetWindow(row.ID, target))

	got, err := s.GetBudget(row.ID)
	require.NoError(t, err)
	assert.Zero(t, got.TotalCost, "the new window starts empty")
	assert.Zero(t, got.RequestCount)
	assert.Equal(t, target, got.LastBudgetRefreshAt.UTC())
}

// ---------- DeleteBudget ----------

func TestDeleteBudget(t *testing.T) {
	s := store(t)
	row := newBudgetRow(t, s, 1_000)

	require.NoError(t, s.DeleteBudget(row.ID))

	_, err := s.GetBudget(row.ID)
	assert.Error(t, err)
	assert.Error(t, s.DeleteBudget(row.ID), "deleting twice must report the miss")
}
