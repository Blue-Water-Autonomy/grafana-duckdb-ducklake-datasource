-- Administrator setup: the plugin creates this database and runs this file once
-- per data source initialization. The underlying Parquet file is seeded by Compose.
CREATE OR REPLACE VIEW example_metrics AS
SELECT date_trunc('minute', now()) - minutes_ago * INTERVAL '1 minute' AS time,
       device, value
FROM read_parquet('/samples/temperatures.parquet');
