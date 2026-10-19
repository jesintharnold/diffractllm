package metricsengine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"diffractllm/internal/observability/logging"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/duckdb/duckdb-go/v2"
	"go.uber.org/zap"
)

type OLAPStore interface {
	Write(ctx context.Context, events []*Event) error
	Close() error
	GetOverviewTiles(ctx context.Context, from, to time.Time) (*OverviewTiles, error)
	GetRequestSummaryByTime(ctx context.Context, from, to time.Time, bucket time.Duration, loc *time.Location) (*TimeSummary, error)
	GetRequestLogsByTime(ctx context.Context, from, to time.Time, offset, limit int, filter RequestFilter) (*RequestLogPage, error)
	GetRequestFilterOptions(ctx context.Context, from, to time.Time) (*RequestFilterOptions, error)
	GetRequestDetailByID(ctx context.Context, id string) (*RequestDetail, error)
	GetRequestDetailByRequestID(ctx context.Context, requestID string) (*RequestDetail, error)
	GetPayloadByID(ctx context.Context, id string) (*Payload, error)
	GetTopVirtualKeys(ctx context.Context, from, to time.Time, limit int) ([]VirtualKeyUsage, error)
	GetProviderUsage(ctx context.Context, from, to time.Time) ([]ProviderUsage, error)
	GetCredentialUsage(ctx context.Context, provider string, since time.Time) ([]CredentialUsage, error)
	GetVirtualKeyLastUse(ctx context.Context, since time.Time) ([]VirtualKeyLastUse, error)
	GetBudgetSpendByTime(ctx context.Context, budgetID string, from, to time.Time, bucket time.Duration, loc *time.Location) (*BudgetSpend, error)
	Prune(ctx context.Context, eventsBefore, payloadsBefore time.Time) (map[string]int64, error)
}

//go:embed migrations/*.sql
var migrations embed.FS

type appliedMigration struct {
	Version  int
	Checksum string
}

// Migrate applies pending scripts and returns the schema version; it logs only scripts it runs.
func Migrate(ctx context.Context, db *sql.DB, log *zap.Logger) (int, error) {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name VARCHAR NOT NULL,
			checksum VARCHAR NOT NULL,
			applied_at TIMESTAMP NOT NULL
		)
	`); err != nil {
		return 0, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := make(map[int]appliedMigration)
	rows, err := db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var m appliedMigration
		if err := rows.Scan(&m.Version, &m.Checksum); err != nil {
			return 0, err
		}
		applied[m.Version] = m
	}

	if err := rows.Err(); err != nil {
		return 0, err
	}

	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return 0, err
	}

	sort.Strings(files)

	for index, f := range files {
		version, err := parseVersion(f)
		if err != nil {
			return 0, err
		}
		expected := index + 1
		if version != expected {
			return 0, fmt.Errorf("expected migration %04d but found %04d (%s)", expected, version, filepath.Base(f))
		}
	}

	for _, f := range files {
		version, _ := parseVersion(f)
		name := filepath.Base(f)
		sqlBytes, err := migrations.ReadFile(f)
		if err != nil {
			return 0, err
		}
		checksum := sha256Hex(sqlBytes)

		if old, ok := applied[version]; ok {
			if old.Checksum != checksum {
				log.Error("migration checksum mismatch", zap.String("script", name), zap.Int("version", version))
				return 0, fmt.Errorf("migration %04d checksum mismatch (%s was modified)", version, name)
			}
			continue
		}

		start := time.Now()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return 0, err
		}

		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("%s: %w", name, err)
		}

		if _, err := tx.ExecContext(
			ctx, `INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`, version, name, checksum, time.Now(),
		); err != nil {
			tx.Rollback()
			return 0, err
		}

		if err := tx.Commit(); err != nil {
			tx.Rollback()
			return 0, err
		}

		log.Info("migration applied", zap.String("script", name), zap.Int("version", version), zap.Duration("took", time.Since(start).Round(time.Millisecond)))
	}
	return len(files), nil
}

type DuckDBStore struct {
	db     *sql.DB
	Ctx    context.Context
	conn   driver.Conn
	logger *zap.Logger
}

var (
	DuckDBStoreOnce sync.Once
)

func NewDuckDBStore(ctx context.Context) *DuckDBStore {
	return &DuckDBStore{
		logger: logging.For("metrics"),
		Ctx:    ctx,
	}
}

func (ds *DuckDBStore) Init(dbpath string, ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(dbpath), 0o700); err != nil {
		return fmt.Errorf("failed to create the duck DB directory for %q: %w", dbpath, err)
	}

	connector, err := duckdb.NewConnector(dbpath, func(execer driver.ExecerContext) error {
		_, err := execer.ExecContext(ctx, `SET TimeZone = 'UTC'`, nil)
		return err
	})

	if err != nil {
		return err
	}

	ds.db = sql.OpenDB(connector)

	version, err := Migrate(ds.Ctx, ds.db, ds.logger)
	if err != nil {
		ds.db.Close()
		return err
	}

	if ds.conn, err = connector.Connect(ctx); err != nil {
		ds.db.Close()
		return err
	}
	ds.logger.Info("duckdb ready", zap.String("path", dbpath), zap.Int("schema_version", version))
	return nil
}

func (ds *DuckDBStore) Close() error {
	if ds.conn != nil {
		ds.conn.Close()
	}
	if ds.db == nil {
		return nil
	}
	return ds.db.Close()
}

func (ds *DuckDBStore) Write(ctx context.Context, events []*Event) error {
	if len(events) == 0 {
		return nil
	}

	beginner, ok := ds.conn.(driver.ConnBeginTx)
	if !ok {
		return fmt.Errorf("duckdb conn does not support ConnBeginTx")
	}
	tx, err := beginner.BeginTx(ctx, driver.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin metrics tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	eventsApp, err := duckdb.NewAppenderWithColumns(ds.conn, "", "", "events", eventColumnNames[:])
	if err != nil {
		return fmt.Errorf("init events appender: %w", err)
	}
	row := make([]driver.Value, len(eventColumnNames))
	for _, event := range events {
		if err := event.toAppenderValues(row); err != nil {
			_ = eventsApp.Close()
			return fmt.Errorf("event values %s: %w", event.ID, err)
		}
		if err := eventsApp.AppendRow(row...); err != nil {
			_ = eventsApp.Close()
			return fmt.Errorf("append event row %s: %w", event.ID, err)
		}
	}
	if err := eventsApp.Close(); err != nil {
		return fmt.Errorf("close events appender: %w", err)
	}

	if slices.ContainsFunc(events, func(e *Event) bool { return e.Payload != nil }) {
		payloadApp, err := duckdb.NewAppenderWithColumns(ds.conn, "", "", "event_payloads", eventPayloadColNames)
		if err != nil {
			return fmt.Errorf("init payload appender: %w", err)
		}
		for _, event := range events {
			if event.Payload == nil {
				continue
			}
			if err := payloadApp.AppendRow(event.toPayloadValues()...); err != nil {
				_ = payloadApp.Close()
				return fmt.Errorf("append payload row %s: %w", event.ID, err)
			}
		}
		if err := payloadApp.Close(); err != nil {
			return fmt.Errorf("close payload appender: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit metrics tx: %w", err)
	}
	committed = true
	return nil
}
func (ds *DuckDBStore) Prune(ctx context.Context, eventsBefore, payloadsBefore time.Time) (map[string]int64, error) {
	result := make(map[string]int64)
	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin prune transaction: %w", err)
	}
	defer tx.Rollback()

	eventRes, err := tx.ExecContext(ctx, `DELETE FROM events WHERE started_at < ?`, eventsBefore)
	if err != nil {
		return nil, fmt.Errorf("prune events: %w", err)
	}
	result["total_events"], _ = eventRes.RowsAffected()

	payloadRes, err := tx.ExecContext(ctx, `DELETE FROM event_payloads WHERE started_at < ?`, payloadsBefore)
	if err != nil {
		return nil, fmt.Errorf("prune event payloads: %w", err)
	}
	result["total_payload_events"], _ = payloadRes.RowsAffected()

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit prune transaction failed: %w", err)
	}

	return result, nil
}
