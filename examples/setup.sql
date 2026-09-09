-- This script runs transactionally on data source initialization, not per query.
CREATE OR REPLACE VIEW example_metrics AS
SELECT now() - i * INTERVAL '1 minute' AS time,
       'sensor-' || (i % 3)::VARCHAR AS device,
       20 + sin(i / 5.0) * 5 AS value
FROM range(180) AS sample(i);

-- Replace or extend this with views over your local files:
-- CREATE OR REPLACE VIEW measurements AS
-- SELECT * FROM read_parquet('/var/lib/telemetry/*.parquet', union_by_name = true);
