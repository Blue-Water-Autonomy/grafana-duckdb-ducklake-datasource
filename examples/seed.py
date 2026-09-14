"""Initialize both catalogs, backfill sample data, and grant a TLS reader role."""
import os

from catalog import connect, literal

for tls in [False, True]:
    with connect(tls) as db:
        db.execute('CREATE TABLE IF NOT EXISTS lake.measurements (time TIMESTAMPTZ, device VARCHAR, value DOUBLE)')
        db.execute("""
            INSERT INTO lake.measurements
            SELECT date_trunc('minute', now()) - i * INTERVAL '1 minute',
                   'sensor-' || sensor::VARCHAR,
                   19 + sensor * 2 + sin(i / 9.0 + sensor) * 3
            FROM range(120) AS minutes(i), range(1, 4) AS sensors(sensor)
            WHERE NOT EXISTS (SELECT 1 FROM lake.measurements WHERE time > now() - INTERVAL '30 minutes')
        """)
        if tls:
            # The reader can inspect catalog metadata, including future tables.
            # SQL and libpq credentials remain separate throughout this setup.
            statement = "DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'grafana_reader') THEN CREATE ROLE grafana_reader LOGIN; END IF; END $$"
            db.execute("CALL postgres_execute('__ducklake_metadata_lake', " + literal(statement) + ')')
            for statement in [
                'ALTER ROLE grafana_reader PASSWORD ' + literal(os.environ['DUCKLAKE_READER_PASSWORD']),
                'GRANT USAGE ON SCHEMA public TO grafana_reader',
                'GRANT SELECT ON ALL TABLES IN SCHEMA public TO grafana_reader',
                'GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO grafana_reader',
                'ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO grafana_reader',
            ]:
                db.execute("CALL postgres_execute('__ducklake_metadata_lake', " + literal(statement) + ')')
        print(('TLS' if tls else 'Local') + ' DuckLake sample data is ready.', flush=True)
