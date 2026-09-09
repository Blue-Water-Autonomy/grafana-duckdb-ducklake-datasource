package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func testSource(t *testing.T, settings map[string]any) *Datasource {
	t.Helper()
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{JSONData: encoded})
	if err != nil {
		t.Fatal(err)
	}
	ds := instance.(*Datasource)
	t.Cleanup(ds.Dispose)
	return ds
}

func request(sql, format string) *backend.QueryDataRequest {
	encoded, _ := json.Marshal(queryModel{RawSQL: sql, Format: format})
	return &backend.QueryDataRequest{Queries: []backend.DataQuery{{RefID: "A", JSON: encoded, Interval: time.Minute,
		TimeRange: backend.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 123456000, time.UTC), To: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}}}}
}

func result(t *testing.T, ds *Datasource, sql, format string) *data.Frame {
	t.Helper()
	response, err := ds.QueryData(context.Background(), request(sql, format))
	if err != nil {
		t.Fatal(err)
	}
	answer := response.Responses["A"]
	if answer.Error != nil {
		t.Fatal(answer.Error)
	}
	if len(answer.Frames) != 1 {
		t.Fatalf("expected one frame, got %d", len(answer.Frames))
	}
	return answer.Frames[0]
}

func cell(frame *data.Frame, column, row int) any {
	value, _ := frame.Fields[column].ConcreteAt(row)
	return value
}

func TestEmbeddedParquetViewsAndReopen(t *testing.T) {
	root := t.TempDir()
	parquet := filepath.Join(root, "measurements.parquet")
	seed, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = seed.Exec("COPY (SELECT TIMESTAMP '2026-01-01' AS time, 42.5 AS value) TO " + sqlLiteral(parquet) + " (FORMAT PARQUET)")
	seed.Close()
	if err != nil {
		t.Fatal(err)
	}
	setupPath := filepath.Join(root, "setup.sql")
	setup := "CREATE OR REPLACE VIEW measurements AS SELECT * FROM read_parquet(" + sqlLiteral(parquet) + "); CREATE TABLE IF NOT EXISTS setup_runs AS SELECT 1 AS count;"
	if err := os.WriteFile(setupPath, []byte(setup), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "nested", "analytics.duckdb")
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": path, "setupSqlPath": setupPath})
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	frame := result(t, ds, "SELECT * FROM measurements", "time_series")
	if frame.Rows() != 1 || cell(frame, 1, 0) != 42.5 {
		t.Fatalf("unexpected frame: %+v", frame)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); result(t, ds, "SELECT * FROM measurements", "table") }()
	}
	wg.Wait()
	ds.Dispose()
	ro := testSource(t, map[string]any{"mode": ModeReadOnly, "path": path})
	result(t, ro, "SELECT * FROM measurements", "table")
	if _, err := ro.db.Exec("CREATE TABLE forbidden(i INTEGER)"); err == nil {
		t.Fatal("read-only connection accepted DDL")
	}
}

func TestReadOnlyNeverCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "analytics.duckdb")
	encoded, _ := json.Marshal(map[string]any{"mode": ModeReadOnly, "path": path})
	_, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{JSONData: encoded})
	if err == nil || !strings.Contains(err.Error(), "already exist") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("read-only mode created a directory")
	}
}

func TestSetupFailureRollsBackAndReleasesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.duckdb")
	encoded, _ := json.Marshal(map[string]any{"mode": ModeEmbedded, "path": path, "setupSql": "CREATE TABLE should_rollback(i INTEGER); SELECT * FROM missing_table"})
	_, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{JSONData: encoded})
	if err == nil {
		t.Fatal("expected setup failure")
	}
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": path})
	frame := result(t, ds, "SELECT count(*) FROM information_schema.tables WHERE table_name='should_rollback'", "table")
	if cell(frame, 0, 0) != int64(0) {
		t.Fatal("failed setup left a table behind")
	}
}

func TestQueriesRejectWritesAndMultipleStatements(t *testing.T) {
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": filepath.Join(t.TempDir(), "db.duckdb"), "setupSql": "CREATE TABLE measurements AS SELECT 1 AS value"})
	for _, query := range []string{
		"INSERT INTO measurements VALUES (2)", "DELETE FROM measurements", "DROP TABLE measurements",
		"COMMIT; INSERT INTO measurements VALUES (2); SELECT 1", "SELECT 1; SELECT 2",
		"COPY measurements TO '/tmp/forbidden.parquet'", "SET threads=1", "ATTACH ':memory:' AS other",
		"EXPLAIN ANALYZE DELETE FROM measurements",
	} {
		t.Run(query, func(t *testing.T) {
			response, err := ds.QueryData(context.Background(), request(query, "table"))
			if err != nil {
				t.Fatal(err)
			}
			if response.Responses["A"].Error == nil {
				t.Fatal("unsafe query was accepted")
			}
			frame := result(t, ds, "SELECT count(*) FROM measurements", "table")
			if cell(frame, 0, 0) != int64(1) {
				t.Fatal("dashboard query changed persistent data")
			}
		})
	}
	for _, query := range []string{"-- comment\nWITH x AS (SELECT 1 AS value) SELECT * FROM x; -- trailing comment", "SELECT '; DROP TABLE measurements' AS text", "FROM measurements", "DESCRIBE measurements", "SHOW TABLES"} {
		result(t, ds, query, "table")
	}
}

func TestNullableDuckDBTypes(t *testing.T) {
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": filepath.Join(t.TempDir(), "db.duckdb")})
	for _, typ := range []string{"TINYINT", "SMALLINT", "INTEGER", "BIGINT", "UTINYINT", "USMALLINT", "UINTEGER", "UBIGINT", "HUGEINT", "UHUGEINT", "FLOAT", "DOUBLE", "DECIMAL(18,3)"} {
		t.Run(typ, func(t *testing.T) {
			frame := result(t, ds, "SELECT 42::"+typ+" AS value UNION ALL SELECT NULL::"+typ, "table")
			if frame.Rows() != 2 || frame.Fields[0].NilAt(0) || !frame.Fields[0].NilAt(1) {
				t.Fatal("nullable type conversion failed")
			}
		})
	}
	for _, expression := range []string{"TRUE", "TIMESTAMP '2026-01-01'", "TIMESTAMPTZ '2026-01-01 01:00:00+01'", "[1,2,3]", "[1,2,3]::INTEGER[3]", "MAP {1:'a', 2:'b'}", "INTERVAL '3 days'", "'01010'::BIT", "'blob'::BLOB", "{'device':'a','value':42}", "UUID '550e8400-e29b-41d4-a716-446655440000'"} {
		t.Run(expression, func(t *testing.T) {
			frame := result(t, ds, "SELECT "+expression+" AS value UNION ALL SELECT NULL", "table")
			if frame.Rows() != 2 || !frame.Fields[0].NilAt(1) {
				t.Fatal("nullable value conversion failed")
			}
		})
	}
	frame := result(t, ds, "SELECT 170141183460469231731687303715884105727::HUGEINT AS value", "table")
	if cell(frame, 0, 0) != "170141183460469231731687303715884105727" {
		t.Fatal("HUGEINT lost precision")
	}
	frame = result(t, ds, "SELECT UUID '550e8400-e29b-41d4-a716-446655440000', '01010'::BIT, NULL", "table")
	if cell(frame, 0, 0) != "550e8400-e29b-41d4-a716-446655440000" || cell(frame, 1, 0) != "01010" || !frame.Fields[2].NilAt(0) {
		t.Fatal("UUID, bit or untyped NULL conversion is incorrect")
	}
}

func TestTimeSeriesMacrosLabelsAndRowLimit(t *testing.T) {
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": filepath.Join(t.TempDir(), "db.duckdb"), "maxRows": 2})
	frame := result(t, ds, "SELECT $__timeFrom() AS time, 'a' AS device, 42.0 AS value UNION ALL SELECT $__timeTo(), 'b', 7.0 ORDER BY 1", "time_series")
	if frame.Meta.Type != data.FrameTypeTimeSeriesWide || len(frame.Fields) != 3 || frame.Fields[1].Labels["device"] != "a" {
		t.Fatalf("incorrect series: %+v", frame)
	}
	frame = result(t, ds, "SELECT $__timeGroupAlias(TIMESTAMP '2026-01-01 00:01:12', '$__interval'), 1 AS value WHERE $__timeFilter(TIMESTAMP '2026-01-01 00:01:12')", "time_series")
	if frame.Rows() != 1 {
		t.Fatal("macro did not return a bucket")
	}
	frame = result(t, ds, "SELECT * FROM range(10)", "table")
	if frame.Rows() != 2 || len(frame.Meta.Notices) != 1 {
		t.Fatal("row cap must be visible to users")
	}
}

func TestCancellationAndRecovery(t *testing.T) {
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": filepath.Join(t.TempDir(), "db.duckdb"), "maxOpenConns": 1})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	response, err := ds.QueryData(ctx, request("SELECT sum(sqrt(i::DOUBLE)) FROM range(10000000000) t(i)", "table"))
	if err != nil {
		t.Fatal(err)
	}
	if response.Responses["A"].Error == nil {
		t.Fatal("cancelled query unexpectedly succeeded")
	}
	result(t, ds, "SELECT 1", "table")
}

func TestSettingsValidation(t *testing.T) {
	for _, path := range []string{"", "relative.duckdb", "md:my_db", "https://host/file.duckdb", "/tmp/db?access_mode=read_write", "/tmp/db#fragment", ":memory:", "//host/share/db"} {
		t.Run(path, func(t *testing.T) {
			encoded, _ := json.Marshal(map[string]any{"path": path})
			if _, err := loadSettings(backend.DataSourceInstanceSettings{JSONData: encoded}); err == nil {
				t.Fatal("invalid path accepted")
			}
		})
	}
}

func TestTimeSeriesRejectsInvalidTimes(t *testing.T) {
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": filepath.Join(t.TempDir(), "db.duckdb")})
	for _, query := range []string{
		"SELECT NULL::TIMESTAMP AS time, 1 AS value",
		"SELECT TIMESTAMP '2026-01-02' AS time, 1 AS value UNION ALL SELECT TIMESTAMP '2026-01-01', 2",
		"SELECT TIMESTAMP '2026-01-01' AS time, 1 AS value UNION ALL SELECT TIMESTAMP '2026-01-01', 2",
		"SELECT now() AS time, now() AS other_time",
	} {
		response, err := ds.QueryData(context.Background(), request(query, "time_series"))
		if err != nil || response.Responses["A"].Error == nil {
			t.Fatalf("expected invalid time series for %s: %v", query, err)
		}
	}
	frame := result(t, ds, "SELECT 1 AS value, now() AS time", "time_series")
	if frame.Fields[0].Name != "time" {
		t.Fatal("timestamp must be the first time series field")
	}
}

func TestQuotedLocalPathAndEmptyResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quote's and spaces", "db.duckdb")
	ds := testSource(t, map[string]any{"mode": ModeEmbedded, "path": path})
	frame := result(t, ds, "SELECT now() AS time, 1 AS value WHERE false", "time_series")
	if frame.Rows() != 0 || len(frame.Fields) != 2 {
		t.Fatal("empty results lost the schema")
	}
	ds.Dispose()
	ro := testSource(t, map[string]any{"path": path})
	result(t, ro, "SELECT 1", "table")
}
