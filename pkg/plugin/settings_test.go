package plugin

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestPostgresTLSSettings(t *testing.T) {
	for _, tc := range []struct {
		name      string
		config    map[string]any
		wantMode  string
		wantError string
	}{
		{"default verifies hostname", map[string]any{"postgresSslRootCert": "/certs/ca.crt"}, "verify-full", ""},
		{"explicit empty verifies hostname", map[string]any{"postgresSslMode": "", "postgresSslRootCert": "/certs/ca.crt"}, "verify-full", ""},
		{"default needs CA", nil, "", "requires a CA"},
		{"local plaintext", map[string]any{"postgresSslMode": "disable"}, "disable", ""},
		{"encrypted", map[string]any{"postgresSslMode": "require"}, "require", ""},
		{"CA only", map[string]any{"postgresSslMode": "verify-ca", "postgresSslRootCert": "/certs/ca.crt"}, "verify-ca", ""},
		{"mutual TLS", map[string]any{"postgresSslRootCert": "/certs/ca.crt", "postgresSslCert": "/certs/client.crt", "postgresSslKey": "/certs/client.key"}, "verify-full", ""},
		{"unknown mode", map[string]any{"postgresSslMode": "prefer"}, "", "TLS mode must be"},
		{"Unix socket cannot bypass TLS", map[string]any{"postgresHost": "/tmp", "postgresSslRootCert": "/certs/ca.crt"}, "", "single hostname"},
		{"abstract socket cannot bypass TLS", map[string]any{"postgresHost": "@postgres", "postgresSslRootCert": "/certs/ca.crt"}, "", "single hostname"},
		{"empty host fallback cannot bypass TLS", map[string]any{"postgresHost": "catalog.example.com,", "postgresSslRootCert": "/certs/ca.crt"}, "", "single hostname"},
		{"connection injection", map[string]any{"postgresSslMode": "disable host=attacker"}, "", "TLS mode must be"},
		{"missing client key", map[string]any{"postgresSslMode": "require", "postgresSslCert": "/certs/client.crt"}, "", "configured together"},
		{"missing client cert", map[string]any{"postgresSslMode": "require", "postgresSslKey": "/certs/client.key"}, "", "configured together"},
		{"disabled with cert", map[string]any{"postgresSslMode": "disable", "postgresSslRootCert": "/certs/ca.crt"}, "", "require TLS"},
		{"relative CA", map[string]any{"postgresSslRootCert": "ca.crt"}, "", "absolute local"},
		{"remote CA", map[string]any{"postgresSslRootCert": "https://example.com/ca.crt"}, "", "absolute local"},
		{"invalid key path", map[string]any{"postgresSslMode": "require", "postgresSslCert": "/certs/client.crt", "postgresSslKey": "/certs/key\nfile"}, "", "absolute local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := map[string]any{"mode": ModeDuckLake, "postgresHost": "catalog.example.com", "postgresDatabase": "lake", "postgresUser": "reader", "dataPath": "/data/lake"}
			for k, v := range tc.config {
				config[k] = v
			}
			encoded, _ := json.Marshal(config)
			s, err := loadSettings(backend.DataSourceInstanceSettings{JSONData: encoded})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s.PostgresSSLMode != tc.wantMode {
				t.Fatalf("TLS mode = %q", s.PostgresSSLMode)
			}
		})
	}
}

func TestLegacyEmbeddedMode(t *testing.T) {
	ds := testSource(t, map[string]any{"mode": "duckdb-managed", "path": filepath.Join(t.TempDir(), "db.duckdb"), "setupSql": "CREATE VIEW example AS SELECT 42 AS value"})
	if got := cell(result(t, ds, "SELECT * FROM example", "table"), 0, 0); got != int32(42) {
		t.Fatalf("legacy configuration returned %v", got)
	}
}

func TestDefaultEmbeddedMode(t *testing.T) {
	for _, mode := range []any{nil, ""} {
		config := map[string]any{"path": filepath.Join(t.TempDir(), "db.duckdb"), "setupSql": "CREATE VIEW example AS SELECT 42 AS value"}
		if mode != nil {
			config["mode"] = mode
		}
		ds := testSource(t, config)
		if got := cell(result(t, ds, "SELECT * FROM example", "table"), 0, 0); got != int32(42) {
			t.Fatalf("default configuration returned %v", got)
		}
	}
}
