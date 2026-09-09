package plugin

import (
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/gtime"
	"github.com/grafana/grafana-plugin-sdk-go/data/sqlutil"
)

func timestampLiteral(t time.Time) string {
	return "TIMESTAMPTZ " + sqlLiteral(t.UTC().Format(time.RFC3339Nano))
}

func noArguments(args []string) bool {
	return len(args) == 0 || (len(args) == 1 && args[0] == "")
}

func timeGroup(q *sqlutil.Query, args []string) (string, error) {
	if len(args) != 2 || args[0] == "" {
		return "", fmt.Errorf("$__timeGroup requires a column and an interval, e.g. $__timeGroup(time, '5m')")
	}
	interval := strings.Trim(args[1], "'\"")
	duration := q.Interval
	if interval != "$__interval" {
		var err error
		duration, err = gtime.ParseInterval(interval)
		if err != nil {
			return "", fmt.Errorf("invalid time grouping interval: %w", err)
		}
	}
	if duration < time.Microsecond {
		return "", fmt.Errorf("time grouping interval must be positive and at least one microsecond")
	}
	return fmt.Sprintf("time_bucket(INTERVAL '%d microseconds', %s)", duration.Microseconds(), args[0]), nil
}

func interpolateSQL(raw string, query backend.DataQuery) (string, error) {
	q := &sqlutil.Query{RawSQL: raw, TimeRange: query.TimeRange, Interval: query.Interval}
	if q.Interval <= 0 {
		q.Interval = time.Second
	}
	return sqlutil.Interpolate(q, sqlutil.Macros{
		"timeFrom": func(q *sqlutil.Query, args []string) (string, error) {
			if !noArguments(args) {
				return "", fmt.Errorf("$__timeFrom takes no arguments")
			}
			return timestampLiteral(q.TimeRange.From), nil
		},
		"timeTo": func(q *sqlutil.Query, args []string) (string, error) {
			if !noArguments(args) {
				return "", fmt.Errorf("$__timeTo takes no arguments")
			}
			return timestampLiteral(q.TimeRange.To), nil
		},
		"timeFilter": func(q *sqlutil.Query, args []string) (string, error) {
			if len(args) != 1 || args[0] == "" {
				return "", fmt.Errorf("$__timeFilter requires one timestamp column")
			}
			return fmt.Sprintf("(%s BETWEEN %s AND %s)", args[0], timestampLiteral(q.TimeRange.From), timestampLiteral(q.TimeRange.To)), nil
		},
		"timeGroup": timeGroup,
		"timeGroupAlias": func(q *sqlutil.Query, args []string) (string, error) {
			group, err := timeGroup(q, args)
			return group + ` AS "time"`, err
		},
	})
}
