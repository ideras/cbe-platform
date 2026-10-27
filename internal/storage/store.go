package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db *sql.DB
	// backup is where the previous database was copied before this start upgraded it; empty when no upgrade was needed.
	backup string
}

// UpgradeBackup is the file the database was copied to before this start applied new migrations, or "" if none were pending.
func (s *Store) UpgradeBackup() string { return s.backup }

func Open(ctx context.Context, path string) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// One connection serializes transactions; connection-local pragmas also apply on replacement.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := s.verifyJSONB(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if s.backup, err = s.backupBeforeUpgrade(ctx, absolute); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// upgradeClock names the copy made before an upgrade; tests fix it to provoke a clash.
var upgradeClock = func() time.Time { return time.Now().UTC() }

// backupBeforeUpgrade copies an existing database next to itself before pending migrations change it, so a bad upgrade can
// be undone by putting the copy back. A new or up-to-date database needs none. If the copy cannot be made the upgrade does
// not happen. The copy holds the same names, e-mail addresses and exam PINs as the database, so it is private like it.
func (s *Store) backupBeforeUpgrade(ctx context.Context, path string) (string, error) {
	var tables int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&tables); err != nil {
		return "", err
	}
	if tables == 0 {
		return "", nil // a new database has nothing to lose
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return "", err
	}
	first := ""
	for _, entry := range entries {
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version = ?`, entry.Name()).Scan(&applied); err != nil {
			return "", err
		}
		if applied == 0 {
			first = entry.Name()
			break
		}
	}
	if first == "" {
		return "", nil
	}
	backup := path + ".backup-" + upgradeClock().Format("20060102T150405Z") + "-before-" + strings.TrimSuffix(first, ".sql")
	file, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // private from the first byte
	if err != nil {
		return "", fmt.Errorf("cannot back up the database before upgrading it (it was not changed): %w", err)
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, backup); err != nil {
		os.Remove(backup)
		return "", fmt.Errorf("cannot back up the database before upgrading it (it was not changed): %w", err)
	}
	return backup, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SQLiteVersion(ctx context.Context) (string, error) {
	var version string
	err := s.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
	return version, err
}

func (s *Store) verifyJSONB(ctx context.Context) error {
	version, err := s.SQLiteVersion(ctx)
	if err != nil {
		return err
	}
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return fmt.Errorf("unexpected SQLite version %q", version)
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || major < 3 || (major == 3 && minor < 45) {
		return fmt.Errorf("SQLite >= 3.45 required; got %s", version)
	}
	const input = `{"prompt":"¿Qué imprime 世界? 🧪"}`
	var output string
	if err := s.db.QueryRowContext(ctx, "SELECT json(jsonb(?))", input).Scan(&output); err != nil {
		return fmt.Errorf("SQLite JSONB preflight: %w", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(output), &decoded); err != nil || decoded["prompt"] != "¿Qué imprime 世界? 🧪" {
		return fmt.Errorf("SQLite JSONB round-trip failed")
	}
	return nil
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY NOT NULL)`); err != nil {
			return err
		}
		entries, err := migrations.ReadDir("migrations")
		if err != nil {
			return err
		}
		for _, entry := range entries {
			var count int
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version = ?", entry.Name()).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				continue
			}
			script, err := migrations.ReadFile("migrations/" + entry.Name())
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, string(script)); err != nil {
				return fmt.Errorf("migration %s: %w", entry.Name(), err)
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", entry.Name()); err != nil {
				return err
			}
		}
		return nil
	})
}
