package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	duckdb "github.com/duckdb/duckdb-go/v2"
)

func openDatabase(ctx context.Context, s Settings) (*sql.DB, error) {
	path := s.Path
	params := url.Values{"autoinstall_known_extensions": {"false"}, "autoload_known_extensions": {"false"}}
	if s.ExtensionDirectory != "" {
		params.Set("extension_directory", s.ExtensionDirectory)
	}
	switch s.Mode {
	case ModeEmbedded:
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	case ModeDuckLake:
		path = ":memory:"
	}
	connector, err := duckdb.NewConnector(path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("open DuckDB: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(s.MaxOpenConns)
	db.SetMaxIdleConns(s.MaxOpenConns)
	success := false
	defer func() {
		if !success {
			db.Close()
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	if s.Mode == ModeDuckLake {
		if err := attachDuckLake(ctx, db, s); err != nil {
			// Native errors may contain the ATTACH statement, including its libpq DSN.
			// Do not log or return that statement or the database password.
			return nil, fmt.Errorf("connect to DuckLake: %s", redactPostgres(err.Error(), s))
		}
	}
	if s.Mode == ModeEmbedded {
		script := s.SetupSQL
		if s.SetupSQLPath != "" {
			contents, err := os.ReadFile(s.SetupSQLPath)
			if err != nil {
				return nil, fmt.Errorf("read setup SQL file: %w", err)
			}
			script = string(contents)
		}
		if strings.TrimSpace(script) != "" {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return nil, err
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(ctx, script); err != nil {
				return nil, fmt.Errorf("run setup SQL: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("commit setup SQL: %w", err)
			}
		}
	}
	success = true
	return db, nil
}

func attachDuckLake(ctx context.Context, db *sql.DB, s Settings) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, extension := range []string{"ducklake", "postgres"} {
		if _, err := conn.ExecContext(ctx, "LOAD "+extension); err != nil {
			if _, err := conn.ExecContext(ctx, "INSTALL "+extension); err != nil {
				return fmt.Errorf("install %s extension (preinstall it for offline operation): %w", extension, err)
			}
			if _, err := conn.ExecContext(ctx, "LOAD "+extension); err != nil {
				return err
			}
		}
	}
	// A named, temporary secret keeps credentials out of ATTACH statements and query errors.
	if _, err := conn.ExecContext(ctx, "CREATE SECRET local_catalog (TYPE postgres, HOST "+sqlLiteral(s.PostgresHost)+
		fmt.Sprintf(", PORT %d, DATABASE ", s.PostgresPort)+sqlLiteral(s.PostgresDatabase)+", USER "+sqlLiteral(s.PostgresUser)+
		", PASSWORD "+sqlLiteral(s.PostgresPassword)+");"); err != nil {
		return err
	}
	attach := lakeAttachSQL(s)
	if s.CreateDuckLake {
		if err := os.MkdirAll(s.DataPath, 0750); err != nil {
			return fmt.Errorf("create DuckLake data directory: %w", err)
		}
		if _, err := conn.ExecContext(ctx, attach+")"); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "DETACH lake"); err != nil {
			return err
		}
	}
	_, err = conn.ExecContext(ctx, attach+", READ_ONLY, CREATE_IF_NOT_EXISTS false)")
	return err
}

func lakeAttachSQL(s Settings) string {
	mode := s.PostgresSSLMode
	if mode == "" {
		mode = "verify-full"
	}
	// Escape libpq values first, then the complete DuckDB SQL literal. These
	// parameters contain certificate paths only; credentials remain in the secret.
	dsn := "ducklake:postgres:connect_timeout=10 gssencmode=disable sslmode=" + pgLiteral(mode)
	for _, param := range []struct{ key, value string }{
		{"sslrootcert", s.PostgresSSLRootCert},
		{"sslcert", s.PostgresSSLCert},
		{"sslkey", s.PostgresSSLKey},
	} {
		if param.value != "" {
			dsn += " " + param.key + "=" + pgLiteral(param.value)
		}
	}
	return "ATTACH " + sqlLiteral(dsn) + " AS lake (" +
		"METADATA_PARAMETERS MAP {'SECRET': 'local_catalog'}, DATA_PATH " + sqlLiteral(s.DataPath) + ", OVERRIDE_DATA_PATH false"
}

func redactPostgres(message string, s Settings) string {
	if s.PostgresPassword != "" {
		for _, secret := range []string{sqlLiteral(s.PostgresPassword), pgLiteral(s.PostgresPassword), s.PostgresPassword} {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}
