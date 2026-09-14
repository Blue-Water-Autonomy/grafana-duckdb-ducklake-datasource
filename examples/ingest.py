"""Publish fresh measurements to local Parquet files and both DuckLake catalogs."""
from datetime import datetime, timezone
import time

import duckdb

from catalog import connect
from parquet import measurements, publish

connections = [connect(tls=False), connect(tls=True)]
files = duckdb.connect()
try:
    while True:
        rows = measurements(datetime.now(timezone.utc))
        publish(files, rows)
        for db in connections:
            db.executemany('INSERT INTO lake.measurements VALUES (?, ?, ?)', rows)
            # Bound the demo's live history while retaining enough data for the dashboard.
            db.execute("DELETE FROM lake.measurements WHERE time < now() - INTERVAL '6 hours'")
        print('Published measurements to Parquet and both DuckLake catalogs.', flush=True)
        time.sleep(5)
finally:
    files.close()
    for db in connections:
        db.close()
