-- Administrator setup: the plugin creates this database and runs this file once
-- per data source initialization. The ingestion service publishes new Parquet files.
CREATE OR REPLACE VIEW example_metrics AS
SELECT time, device, value
FROM read_parquet('/samples/measurements/*.parquet');
