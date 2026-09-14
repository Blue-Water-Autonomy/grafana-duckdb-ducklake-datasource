package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	ModeEmbedded = "duckdb-embedded"
	ModeDuckLake = "ducklake"
)

type Settings struct {
	Mode                string `json:"mode"`
	Path                string `json:"path"`
	SetupSQL            string `json:"setupSql"`
	SetupSQLPath        string `json:"setupSqlPath"`
	DataPath            string `json:"dataPath"`
	PostgresHost        string `json:"postgresHost"`
	PostgresPort        int    `json:"postgresPort"`
	PostgresDatabase    string `json:"postgresDatabase"`
	PostgresUser        string `json:"postgresUser"`
	PostgresPassword    string `json:"-"`
	PostgresSSLMode     string `json:"postgresSslMode"`
	PostgresSSLRootCert string `json:"postgresSslRootCert"`
	PostgresSSLCert     string `json:"postgresSslCert"`
	PostgresSSLKey      string `json:"postgresSslKey"`
	CreateDuckLake      bool   `json:"createDuckLake"`
	ExtensionDirectory  string `json:"extensionDirectory"`
	MaxOpenConns        int    `json:"maxOpenConns"`
	MaxRows             int64  `json:"maxRows"`
	QueryTimeout        int    `json:"queryTimeout"`
}

func loadSettings(source backend.DataSourceInstanceSettings) (Settings, error) {
	s := Settings{Mode: ModeEmbedded, MaxOpenConns: 4, MaxRows: 100000, QueryTimeout: 30, PostgresHost: "localhost", PostgresPort: 5432}
	if len(source.JSONData) > 0 {
		if err := json.Unmarshal(source.JSONData, &s); err != nil {
			return s, fmt.Errorf("invalid data source settings: %w", err)
		}
	}
	s.PostgresPassword = source.DecryptedSecureJSONData["postgresPassword"]
	if s.Mode == "" {
		s.Mode = ModeEmbedded
	}
	// Accept existing provisioning while presenting the new name everywhere.
	if s.Mode == "duckdb-managed" {
		s.Mode = ModeEmbedded
	}
	if s.PostgresSSLMode == "" {
		s.PostgresSSLMode = "verify-full"
	}
	if s.MaxOpenConns < 1 || s.MaxOpenConns > 32 {
		return s, fmt.Errorf("maximum connections must be between 1 and 32")
	}
	if s.MaxRows < 1 || s.MaxRows > 1000000 {
		return s, fmt.Errorf("maximum rows must be between 1 and 1000000")
	}
	if s.QueryTimeout < 1 || s.QueryTimeout > 600 {
		return s, fmt.Errorf("query timeout must be between 1 and 600 seconds")
	}
	if s.ExtensionDirectory == "" && os.Getenv("GF_PATHS_DATA") != "" {
		s.ExtensionDirectory = filepath.Join(os.Getenv("GF_PATHS_DATA"), "duckdb", "extensions")
	}
	if s.ExtensionDirectory != "" {
		if err := localPath("extension directory", s.ExtensionDirectory); err != nil {
			return s, err
		}
	}
	switch s.Mode {
	case ModeEmbedded:
		if err := localPath("database path", s.Path); err != nil {
			return s, err
		}
		if s.SetupSQL != "" && s.SetupSQLPath != "" {
			return s, fmt.Errorf("choose inline setup SQL or a setup SQL file, not both")
		}
		if s.SetupSQLPath != "" {
			if err := localPath("setup SQL file", s.SetupSQLPath); err != nil {
				return s, err
			}
		}
	case ModeDuckLake:
		if s.SetupSQL != "" || s.SetupSQLPath != "" {
			return s, fmt.Errorf("setup SQL is only available in embedded DuckDB mode")
		}
		if s.PostgresHost == "" || s.PostgresDatabase == "" || s.PostgresUser == "" {
			return s, fmt.Errorf("DuckLake requires a PostgreSQL host, database, and user")
		}
		if strings.ContainsAny(s.PostgresHost+s.PostgresDatabase+s.PostgresUser+s.PostgresPassword, "\x00\r\n") {
			return s, fmt.Errorf("PostgreSQL settings cannot contain NUL or newline characters")
		}
		// libpq ignores sslmode on Unix sockets, including an empty host in a
		// multi-host list. Keep this setting to a single TCP hostname or IP.
		if strings.ContainsAny(s.PostgresHost, "/,@ \t") {
			return s, fmt.Errorf("PostgreSQL host must be a single hostname or IP address; Unix sockets are not supported")
		}
		if s.PostgresPort < 1 || s.PostgresPort > 65535 {
			return s, fmt.Errorf("PostgreSQL port must be between 1 and 65535")
		}
		if err := s.validatePostgresTLS(); err != nil {
			return s, err
		}
		if err := localPath("DuckLake data path", s.DataPath); err != nil {
			return s, err
		}
	default:
		return s, fmt.Errorf("unknown connection mode %q", s.Mode)
	}
	return s, nil
}

func (s Settings) validatePostgresTLS() error {
	switch s.PostgresSSLMode {
	case "disable":
		if s.PostgresSSLRootCert != "" || s.PostgresSSLCert != "" || s.PostgresSSLKey != "" {
			return fmt.Errorf("PostgreSQL certificate files require TLS to be enabled")
		}
	case "require", "verify-ca", "verify-full":
	default:
		return fmt.Errorf("PostgreSQL TLS mode must be disable, require, verify-ca, or verify-full")
	}
	if (s.PostgresSSLMode == "verify-ca" || s.PostgresSSLMode == "verify-full") && s.PostgresSSLRootCert == "" {
		return fmt.Errorf("PostgreSQL TLS mode %s requires a CA certificate file", s.PostgresSSLMode)
	}
	if (s.PostgresSSLCert == "") != (s.PostgresSSLKey == "") {
		return fmt.Errorf("PostgreSQL client certificate and private key files must be configured together")
	}
	for _, file := range []struct{ label, path string }{
		{"PostgreSQL CA certificate file", s.PostgresSSLRootCert},
		{"PostgreSQL client certificate file", s.PostgresSSLCert},
		{"PostgreSQL client private key file", s.PostgresSSLKey},
	} {
		if file.path != "" {
			if err := localPath(file.label, file.path); err != nil {
				return err
			}
		}
	}
	return nil
}

func localPath(label, path string) error {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\x00\r\n") {
		return fmt.Errorf("%s must be an absolute local filesystem path without connection parameters", label)
	}
	return nil
}

func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func pgLiteral(value string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(value) + "'"
}
