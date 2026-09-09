"""A separate writer using the same local PostgreSQL catalog and Parquet volume."""
import os
import time

import duckdb


def literal(value):
    return "'" + value.replace("'", "''") + "'"


connection = duckdb.connect(config={"extension_directory": "/opt/duckdb/extensions"})
connection.execute("LOAD ducklake; LOAD postgres")
connection.execute(
    "CREATE SECRET writer (TYPE postgres, HOST "
    + literal(os.environ.get("DUCKLAKE_HOST", "localhost"))
    + ", DATABASE 'ducklake', USER 'ducklake', PASSWORD "
    + literal(os.environ["DUCKLAKE_PASSWORD"])
    + ")"
)
connection.execute("""
    ATTACH 'ducklake:postgres:sslmode=disable' AS lake (
        METADATA_PARAMETERS MAP {'SECRET': 'writer'},
        DATA_PATH '/var/lib/ducklake/data', OVERRIDE_DATA_PATH false
    )
""")
connection.execute("CREATE TABLE IF NOT EXISTS lake.measurements (time TIMESTAMPTZ, device VARCHAR, value DOUBLE)")
while True:
    connection.execute("INSERT INTO lake.measurements SELECT now(), 'sensor-1', 20 + random() * 5")
    print("Inserted a measurement", flush=True)
    time.sleep(5)
