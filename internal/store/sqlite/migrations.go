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

	applied, err := store.appliedMigrations(ctx)
	if err != nil {
		return err
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
	for _, entry := range entries {
		version, err := migrationVersion(entry)
		if err != nil {
			return nil, err
		}

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
	rows, err := store.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}

	return applied, rows.Err()
}

func (store *Store) applyMigration(ctx context.Context, migration migration) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackOnError(tx)

	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		return err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		migration.Version,
		migration.Name,
		formatTime(store.now()),
	); err != nil {
		return err
	}

	return tx.Commit()
}

func rollbackOnError(tx *sql.Tx) {
	_ = tx.Rollback()
}
