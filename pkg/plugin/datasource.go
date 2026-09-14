package plugin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	duckdb "github.com/duckdb/duckdb-go/v2"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana-plugin-sdk-go/data/sqlutil"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

type Datasource struct {
	mu       sync.RWMutex
	db       *sql.DB
	settings Settings
}

func NewDatasource(ctx context.Context, source backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	s, err := loadSettings(source)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.QueryTimeout)*time.Second)
	defer cancel()
	db, err := openDatabase(ctx, s)
	if err != nil {
		return nil, err
	}
	return &Datasource{db: db, settings: s}, nil
}

func (d *Datasource) Dispose() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db != nil {
		d.db.Close()
		d.db = nil
	}
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	response := backend.NewQueryDataResponse()
	for _, query := range req.Queries {
		response.Responses[query.RefID] = d.query(ctx, query)
	}
	return response, nil
}

type queryModel struct {
	RawSQL string `json:"rawSql"`
	Format string `json:"format"`
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var model queryModel
	if err := json.Unmarshal(query.JSON, &model); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "invalid query JSON")
	}
	if strings.TrimSpace(model.RawSQL) == "" {
		return backend.DataResponse{}
	}
	if model.Format != "" && model.Format != "table" && model.Format != "time_series" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "format must be table or time_series")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.settings.QueryTimeout)*time.Second)
	defer cancel()
	rawSQL, err := interpolateSQL(model.RawSQL, query)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
	}
	frame, err := d.runQuery(ctx, rawSQL)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, redactPostgres(err.Error(), d.settings))
	}
	frame.RefID = query.RefID
	frame.Name = query.RefID
	if frame.Meta == nil {
		frame.Meta = &data.FrameMeta{}
	}
	frame.Meta.ExecutedQueryString = rawSQL
	if model.Format == "time_series" {
		frame, err = timeSeriesFrame(frame)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusBadRequest, err.Error())
		}
	} else {
		frame.Meta.PreferredVisualization = data.VisTypeTable
	}
	return backend.DataResponse{Frames: data.Frames{frame}}
}

func (d *Datasource) runQuery(ctx context.Context, rawSQL string) (*data.Frame, error) {
	if d.db == nil {
		return nil, fmt.Errorf("data source is closed")
	}
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET TimeZone='UTC'"); err != nil {
		return nil, err
	}
	if d.settings.Mode == ModeDuckLake {
		if _, err := conn.ExecContext(ctx, "USE lake"); err != nil {
			return nil, err
		}
	}
	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION READ ONLY"); err != nil {
		return nil, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanupCtx, "ROLLBACK"); err != nil {
			// A cancelled/failed transaction must never be returned to the pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	// Conn.Prepare (unlike PrepareContext/QueryContext) rejects multiple statements
	// without executing their prefixes. Let DuckDB parse SQL, including comments,
	// string literals and CTEs, rather than trying to police it with a regexp.
	err = conn.Raw(func(raw any) error {
		stmt, err := raw.(*duckdb.Conn).Prepare(rawSQL)
		if err != nil {
			return fmt.Errorf("dashboard queries must contain one SELECT statement: %w", err)
		}
		defer stmt.Close()
		kind, err := stmt.(*duckdb.Stmt).StatementType()
		if err != nil {
			return err
		}
		if kind != duckdb.STATEMENT_TYPE_SELECT {
			return fmt.Errorf("dashboard queries must be SELECT statements; use embedded setup SQL for initialization")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rows, err := conn.QueryContext(ctx, rawSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return sqlutil.FrameFromRows(rows, d.settings.MaxRows, converters()...)
}

func timeSeriesFrame(frame *data.Frame) (*data.Frame, error) {
	schema := frame.TimeSeriesSchema()
	if schema.Type == data.TimeSeriesTypeNot {
		return nil, fmt.Errorf("time series requires a timestamp column and at least one numeric column; order by time ascending")
	}
	for _, index := range schema.ValueIndices {
		if !frame.Fields[index].Type().Numeric() {
			return nil, fmt.Errorf("time series value column %q must be numeric", frame.Fields[index].Name)
		}
	}
	var previous time.Time
	for row := 0; row < frame.Rows(); row++ {
		value, valid := frame.Fields[schema.TimeIndex].ConcreteAt(row)
		if !valid {
			return nil, fmt.Errorf("time series timestamps cannot be NULL")
		}
		current := value.(time.Time)
		if row > 0 && (current.Before(previous) || (schema.Type == data.TimeSeriesTypeWide && current.Equal(previous))) {
			return nil, fmt.Errorf("time series must be ordered by time ascending with one value per series and timestamp")
		}
		previous = current
	}
	// Grafana's wide time series contract puts the time field first.
	if schema.TimeIndex != 0 {
		timeField := frame.Fields[schema.TimeIndex]
		frame.Fields = append(frame.Fields[:schema.TimeIndex], frame.Fields[schema.TimeIndex+1:]...)
		frame.Fields = append([]*data.Field{timeField}, frame.Fields...)
	}
	if schema.Type == data.TimeSeriesTypeLong {
		wide, err := data.LongToWide(frame, &data.FillMissing{Mode: data.FillModeNull})
		if err != nil {
			return nil, fmt.Errorf("format time series (order by time ascending): %w", err)
		}
		wide.RefID = frame.RefID
		wide.Meta.ExecutedQueryString = frame.Meta.ExecutedQueryString
		wide.Meta.Notices = frame.Meta.Notices
		frame = wide
	}
	frame.Meta.Type = data.FrameTypeTimeSeriesWide
	frame.Meta.PreferredVisualization = data.VisTypeGraph
	return frame, nil
}

func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.settings.QueryTimeout)*time.Second)
	defer cancel()
	query := "SELECT 1 AS connected"
	if d.settings.Mode == ModeDuckLake {
		// This exercises the PostgreSQL catalog as well as the embedded engine.
		query = "SELECT count(*) FROM information_schema.tables WHERE table_catalog = 'lake'"
	}
	if _, err := d.runQuery(ctx, query); err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: redactPostgres(err.Error(), d.settings)}, nil
	}
	message := "DuckDB is ready (read-only queries)."
	if d.settings.Mode == ModeEmbedded {
		message = "Embedded DuckDB is ready; setup SQL completed."
	} else if d.settings.Mode == ModeDuckLake {
		message = "DuckLake is ready (PostgreSQL catalog, read-only queries)."
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: message}, nil
}
