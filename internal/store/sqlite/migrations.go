package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	Version int
	Name    string
	SQL     string
}

type migrationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) Migrate(ctx context.Context) error {
	if _, err := store.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	appliedNames, err := store.appliedMigrationNames(ctx)
	if err != nil {
		return err
	}
	if err := store.repairLegacyMigrationCollisions(ctx, migrations, appliedNames); err != nil {
		return err
	}

	applied := make(map[int]bool, len(appliedNames))
	for version := range appliedNames {
		applied[version] = true
	}
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}

		if err := store.applyMigration(ctx, migration); err != nil {
			return err
		}
	}

	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, err
	}

	var migrations []migration
	seenVersions := make(map[int]string)
	for _, entry := range entries {
		version, err := migrationVersion(entry)
		if err != nil {
			return nil, err
		}
		if existing, ok := seenVersions[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %04d: %s and %s", version, existing, path.Base(entry))
		}
		seenVersions[version] = path.Base(entry)

		sqlBytes, err := migrationFiles.ReadFile(entry)
		if err != nil {
			return nil, err
		}

		migrations = append(migrations, migration{
			Version: version,
			Name:    path.Base(entry),
			SQL:     string(sqlBytes),
		})
	}

	sort.Slice(migrations, func(i int, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

func migrationVersion(filename string) (int, error) {
	base := path.Base(filename)
	prefix := strings.SplitN(base, "_", 2)[0]
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("invalid migration name %q: %w", filename, err)
	}
	return version, nil
}

func (store *Store) appliedMigrations(ctx context.Context) (map[int]bool, error) {
	names, err := store.appliedMigrationNames(ctx)
	if err != nil {
		return nil, err
	}
	applied := make(map[int]bool, len(names))
	for version := range names {
		applied[version] = true
	}
	return applied, nil
}

func (store *Store) appliedMigrationNames(ctx context.Context) (map[int]string, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT version, name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]string)
	for rows.Next() {
		var (
			version int
			name    string
		)
		if err := rows.Scan(&version, &name); err != nil {
			return nil, err
		}
		applied[version] = name
	}

	return applied, rows.Err()
}

func (store *Store) repairLegacyMigrationCollisions(ctx context.Context, migrations []migration, applied map[int]string) error {
	// Early live databases used versions 11 and 12 for different migration files.
	// Apply the skipped current SQL idempotently so later workspace migrations can run.
	repairs := []struct {
		version      int
		currentName  string
		markerTable  string
		markerColumn string
	}{
		{version: 11, currentName: "0011_workspaces.sql", markerTable: "workspace_policies"},
		{version: 12, currentName: "0012_initiatives.sql", markerTable: "initiatives"},
		{version: 17, currentName: "0017_runtime_state.sql", markerTable: "runtime_state"},
		{version: 18, currentName: "0018_task_queue_fields.sql", markerTable: "tasks", markerColumn: "next_eligible_at"},
		{version: 19, currentName: "0019_workspace_profile.sql", markerTable: "workspace_profile"},
	}

	for _, repair := range repairs {
		appliedName, ok := applied[repair.version]
		if !ok || appliedName == repair.currentName {
			continue
		}
		repaired, err := store.hasLegacyRepairMarker(ctx, repair.markerTable, repair.markerColumn)
		if err != nil {
			return err
		}
		if repaired {
			continue
		}
		migration, ok := migrationByVersion(migrations, repair.version)
		if !ok {
			return fmt.Errorf("migration version %d not found for legacy repair", repair.version)
		}
		if err := store.applyMigrationSQL(ctx, migration); err != nil {
			return fmt.Errorf("repair legacy migration collision %d (%s already recorded): %w", repair.version, appliedName, err)
		}
	}

	return nil
}

func (store *Store) hasLegacyRepairMarker(ctx context.Context, tableName string, columnName string) (bool, error) {
	exists, err := store.HasTable(ctx, tableName)
	if err != nil {
		return false, err
	}
	if !exists || columnName == "" {
		return exists, nil
	}

	var found int
	if err := store.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM pragma_table_info(?)
			WHERE name = ?
		)
	`, tableName, columnName).Scan(&found); err != nil {
		return false, err
	}
	return found == 1, nil
}

func migrationByVersion(migrations []migration, version int) (migration, bool) {
	for _, migration := range migrations {
		if migration.Version == version {
			return migration, true
		}
	}
	return migration{}, false
}

func (store *Store) applyMigrationSQL(ctx context.Context, migration migration) error {
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	committed := false
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer rollbackImmediate(ctx, conn, &committed)

	if _, err := conn.ExecContext(ctx, migration.SQL); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func (store *Store) applyMigration(ctx context.Context, migration migration) error {
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	committed := false
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer rollbackImmediate(ctx, conn, &committed)

	applied, err := migrationAlreadyApplied(ctx, conn, migration.Version)
	if err != nil {
		return err
	}
	if applied {
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			return err
		}
		committed = true
		return nil
	}

	satisfied, err := migrationSchemaAlreadySatisfied(ctx, conn, migration)
	if err != nil {
		return err
	}
	if satisfied {
		if err := store.recordMigrationApplied(ctx, conn, migration); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			return err
		}
		committed = true
		return nil
	}

	if _, err := conn.ExecContext(ctx, migration.SQL); err != nil {
		return err
	}

	if err := store.recordMigrationApplied(ctx, conn, migration); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func migrationAlreadyApplied(ctx context.Context, tx migrationExecutor, version int) (bool, error) {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)`, version).Scan(&exists); err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (store *Store) recordMigrationApplied(ctx context.Context, tx migrationExecutor, migration migration) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		migration.Version,
		migration.Name,
		formatTime(store.now()),
	)
	return err
}

func migrationSchemaAlreadySatisfied(ctx context.Context, tx migrationExecutor, migration migration) (bool, error) {
	switch migration.Version {
	case 46:
		return tableHasColumns(ctx, tx, "approvals", "policy_snapshot_hash", "runtime_snapshot_hash")
	default:
		return false, nil
	}
}

func tableHasColumns(ctx context.Context, tx migrationExecutor, tableName string, columnNames ...string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, tableName)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	found := make(map[string]bool, len(columnNames))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, columnName := range columnNames {
		if !found[columnName] {
			return false, nil
		}
	}
	return true, nil
}

func rollbackImmediate(ctx context.Context, conn *sql.Conn, committed *bool) {
	if *committed {
		return
	}
	_, _ = conn.ExecContext(ctx, `ROLLBACK`)
}

func rollbackOnError(tx *sql.Tx) {
	_ = tx.Rollback()
}
