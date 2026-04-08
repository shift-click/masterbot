package sqliteutil

import (
	"database/sql"
	"fmt"
	"runtime"

	_ "modernc.org/sqlite"
)

// productionPragmas are applied to every new SQLite connection.
const productionPragmas = `
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;
PRAGMA mmap_size = 268435456;
PRAGMA temp_store = MEMORY;
PRAGMA foreign_keys = ON;
`

// SQLiteDB wraps a read and write connection pool.
type SQLiteDB struct {
	Write *sql.DB
	Read  *sql.DB
}

// OpenSQLiteDB opens a SQLite database with separate read/write pools.
// Write pool: MaxOpenConns=1, _txlock=immediate
// Read pool: MaxOpenConns=max(4, GOMAXPROCS), mode=ro
func OpenSQLiteDB(dbPath string) (*SQLiteDB, error) {
	// Write pool
	writeDB, err := sql.Open("sqlite", dbPath+"?_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open sqlite write pool: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)

	if err := applyPragmas(writeDB); err != nil {
		writeDB.Close()
		return nil, err
	}

	// Read pool
	readConns := max(4, runtime.GOMAXPROCS(0))
	readDB, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		writeDB.Close()
		return nil, fmt.Errorf("open sqlite read pool: %w", err)
	}
	readDB.SetMaxOpenConns(readConns)
	readDB.SetMaxIdleConns(readConns)

	if err := applyPragmas(readDB); err != nil {
		writeDB.Close()
		readDB.Close()
		return nil, err
	}

	return &SQLiteDB{Write: writeDB, Read: readDB}, nil
}

func applyPragmas(db *sql.DB) error {
	_, err := db.Exec(productionPragmas)
	if err != nil {
		return fmt.Errorf("apply sqlite pragmas: %w", err)
	}
	return nil
}

// Close closes both read and write pools.
func (s *SQLiteDB) Close() error {
	var firstErr error
	if s.Read != nil {
		if err := s.Read.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.Write != nil {
		if err := s.Write.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Optimize runs PRAGMA optimize. Call at shutdown.
func (s *SQLiteDB) Optimize() error {
	_, err := s.Write.Exec("PRAGMA optimize")
	return err
}

// OptimizeFull runs PRAGMA optimize=0x10002 for full analysis. Call at startup.
func (s *SQLiteDB) OptimizeFull() error {
	_, err := s.Write.Exec("PRAGMA optimize=0x10002")
	return err
}
