package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestDuckLakeConcurrentProcess(t *testing.T) {
	s := duckLakeTestSettings(t)
	encoded, _ := json.Marshal(s)
	instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{JSONData: encoded, DecryptedSecureJSONData: map[string]string{"postgresPassword": s.PostgresPassword}})
	if err != nil {
		t.Fatal(err)
	}
	ds := instance.(*Datasource)
	defer ds.Dispose()
	health, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil || health.Status != backend.HealthStatusOk {
		t.Fatalf("health: %+v, %v", health, err)
	}
	if _, err := ds.db.Exec("CREATE TABLE lake.forbidden(i INTEGER)"); err == nil {
		t.Fatal("DuckLake attachment accepted a write")
	}
	command := exec.Command(os.Args[0], "-test.run=^TestDuckLakeWriterProcess$", "-test.v")
	command.Env = append(os.Environ(), "DUCKLAKE_WRITER_SETTINGS="+string(encoded))
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer command.Process.Kill()
	sawPartial := false
	deadline := time.After(60 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			frame := result(t, ds, "SELECT count(*) FROM measurements", "table")
			if cell(frame, 0, 0) != int64(20) {
				t.Fatalf("expected 20 ingested rows: %v", cell(frame, 0, 0))
			}
			if !sawPartial {
				t.Fatal("did not observe intermediate commits while writer was active")
			}
			result(t, ds, "SELECT time, device, value FROM measurements ORDER BY time", "time_series")
			return
		case <-deadline:
			t.Fatal("writer timed out")
		case <-ticker.C:
			response, err := ds.QueryData(context.Background(), request("SELECT count(*) FROM measurements", "table"))
			if err != nil {
				t.Fatal(err)
			}
			answer := response.Responses["A"]
			if answer.Error != nil {
				continue
			} // The writer creates the table first.
			count := cell(answer.Frames[0], 0, 0).(int64)
			if count > 0 && count < 20 {
				sawPartial = true
			}
		}
	}
}

func duckLakeTestSettings(t *testing.T) Settings {
	t.Helper()
	if os.Getenv("DUCKLAKE_TEST_PORT") == "" {
		t.Skip("run make integration to test against isolated PostgreSQL")
	}
	port, err := strconv.Atoi(os.Getenv("DUCKLAKE_TEST_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	s := Settings{Mode: ModeDuckLake, PostgresHost: "127.0.0.1", PostgresPort: port, PostgresUser: "test", PostgresDatabase: "test",
		PostgresSSLMode:  "disable",
		PostgresPassword: os.Getenv("DUCKLAKE_TEST_PASSWORD"), DataPath: t.TempDir(), CreateDuckLake: true, MaxOpenConns: 4, MaxRows: 1000, QueryTimeout: 120}
	if path := os.Getenv("DUCKLAKE_TEST_DATA"); path != "" {
		s.DataPath = path
	}
	if certDir := os.Getenv("DUCKLAKE_TEST_CERTS"); certDir != "" {
		s.PostgresHost = "localhost"
		s.PostgresSSLMode = "verify-full"
		s.PostgresSSLRootCert = filepath.Join(certDir, "ca.crt")
		s.PostgresSSLCert = filepath.Join(certDir, "client.crt")
		s.PostgresSSLKey = filepath.Join(certDir, "client.key")
	}
	return s
}

func TestDuckLakeTLS(t *testing.T) {
	s := duckLakeTestSettings(t)
	if s.PostgresSSLMode == "disable" {
		s.PostgresSSLMode = "require"
		_, err := openDatabase(context.Background(), s)
		if err == nil || !strings.Contains(err.Error(), "does not support SSL") {
			t.Fatalf("TLS must not fall back to plaintext: %v", err)
		}
		return
	}
	for _, tc := range []struct {
		name      string
		configure func(*Settings)
		wantError string
	}{
		{"verify-full", func(s *Settings) {}, ""},
		{"verify-ca", func(s *Settings) { s.PostgresSSLMode = "verify-ca"; s.PostgresHost = "127.0.0.1" }, ""},
		{"require", func(s *Settings) { s.PostgresSSLMode = "require"; s.PostgresSSLRootCert = "" }, ""},
		{"wrong hostname", func(s *Settings) { s.PostgresHost = "127.0.0.1" }, "does not match host name"},
		{"untrusted CA", func(s *Settings) {
			s.PostgresSSLRootCert = filepath.Join(os.Getenv("DUCKLAKE_TEST_CERTS"), "untrusted.crt")
		}, "certificate verify failed"},
		{"missing client certificate", func(s *Settings) { s.PostgresSSLCert = ""; s.PostgresSSLKey = "" }, "requires a valid client certificate"},
		{"plaintext rejected", func(s *Settings) {
			s.PostgresSSLMode = "disable"
			s.PostgresSSLRootCert = ""
			s.PostgresSSLCert = ""
			s.PostgresSSLKey = ""
		}, "rejects connection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := s
			tc.configure(&settings)
			encoded, _ := json.Marshal(settings)
			instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{JSONData: encoded, DecryptedSecureJSONData: map[string]string{"postgresPassword": settings.PostgresPassword}})
			if tc.wantError != "" {
				if err == nil {
					instance.(*Datasource).Dispose()
					t.Fatal("unexpected successful connection")
				}
				if !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q, got %v", tc.wantError, err)
				}
				if strings.Contains(err.Error(), settings.PostgresPassword) {
					t.Fatal("connection error exposed password")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ds := instance.(*Datasource)
			defer ds.Dispose()
			health, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
			if err != nil || health.Status != backend.HealthStatusOk {
				t.Fatalf("health: %+v, %v", health, err)
			}
			// Inspect the server's live catalog sessions, including the client identity.
			frame := result(t, ds, "SELECT ssl, client_dn FROM postgres_query('__ducklake_metadata_lake', 'SELECT ssl, client_dn FROM pg_stat_ssl WHERE pid = pg_backend_pid()')", "table")
			if cell(frame, 0, 0) != true || !strings.Contains(cell(frame, 1, 0).(string), "CN=test") {
				t.Fatalf("expected mutual TLS session: %+v", frame)
			}
		})
	}
}

func TestDuckLakeWriterProcess(t *testing.T) {
	encoded := os.Getenv("DUCKLAKE_WRITER_SETTINGS")
	if encoded == "" {
		t.Skip("subprocess helper")
	}
	var s Settings
	if err := json.Unmarshal([]byte(encoded), &s); err != nil {
		t.Fatal(err)
	}
	s.PostgresPassword = os.Getenv("DUCKLAKE_TEST_PASSWORD")
	s.CreateDuckLake = false
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := attachDuckLake(context.Background(), db, s); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DETACH lake; " + lakeAttachSQL(s) + "); CREATE TABLE lake.measurements (time TIMESTAMPTZ, device VARCHAR, value DOUBLE)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := db.Exec("INSERT INTO lake.measurements VALUES (now(), 'sensor', ?)", i); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
