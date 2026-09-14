"""Publish immutable Parquet batches for the embedded DuckDB views."""
from datetime import datetime, timedelta, timezone
from pathlib import Path
import math
import uuid

import duckdb


DIRECTORY = Path('/samples/measurements')


def measurements(at):
    return [(at, f'sensor-{sensor}',
             19 + sensor * 2 + math.sin(at.timestamp() / 90 + sensor) * 3)
            for sensor in range(1, 4)]


def publish(db, rows):
    # Staging files do not match the readers' *.parquet glob. Rename only after
    # COPY closes the completed file; published batches are never overwritten.
    target = DIRECTORY / f'{uuid.uuid4().hex}.parquet'
    staging = target.with_suffix('.tmp')
    db.execute('CREATE OR REPLACE TEMP TABLE batch (time TIMESTAMPTZ, device VARCHAR, value DOUBLE)')
    db.executemany('INSERT INTO batch VALUES (?, ?, ?)', rows)
    db.execute(f"COPY batch TO '{staging}' (FORMAT PARQUET)")
    staging.chmod(0o644)
    staging.replace(target)


def seed():
    with duckdb.connect() as db:
        now = datetime.now(timezone.utc)
        if any(DIRECTORY.glob('*.parquet')):
            newest = db.execute(f"SELECT max(time) FROM read_parquet('{DIRECTORY}/*.parquet')").fetchone()[0]
            if newest is not None and newest > now - timedelta(minutes=30):
                return
        rows = [row for minute in range(120)
                for row in measurements(now - timedelta(minutes=minute))]
        publish(db, rows)
