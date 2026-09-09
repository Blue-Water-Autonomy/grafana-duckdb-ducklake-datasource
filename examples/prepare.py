"""Create immutable sample files and demo TLS certificates before readers start."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile

import duckdb


SAMPLE_SQL = """
SELECT i::INTEGER AS minutes_ago,
       'sensor-' || sensor::VARCHAR AS device,
       (19 + sensor * 2 + sin(i / 9.0 + sensor) * 3)::DOUBLE AS value
FROM range(120) AS minutes(i), range(1, 4) AS sensors(sensor)
"""

samples = Path('/samples')
samples.mkdir(exist_ok=True)
for name in ['local', 'tls']:
    directory = Path('/var/lib/ducklake') / name
    directory.mkdir(parents=True, exist_ok=True)
    os.chown(directory, 472, 0)
    directory.chmod(0o750)

snapshot = samples / 'snapshot.duckdb'
if not snapshot.exists():
    temporary = samples / 'snapshot-building.duckdb'
    # A prior interrupted preparation has no readers and can be rebuilt safely.
    temporary.unlink(missing_ok=True)
    Path(str(temporary) + '.wal').unlink(missing_ok=True)
    with duckdb.connect(str(temporary)) as db:
        db.execute('CREATE TABLE samples AS ' + SAMPLE_SQL)
        db.execute("""CREATE VIEW measurements AS
            SELECT date_trunc('minute', now()) - minutes_ago * INTERVAL '1 minute' AS time,
                   device, value FROM samples""")
    temporary.chmod(0o644)
    temporary.replace(snapshot)

parquet = samples / 'temperatures.parquet'
if not parquet.exists():
    with duckdb.connect() as db:
        db.execute("COPY (" + SAMPLE_SQL + ") TO '/samples/temperatures-building.parquet' (FORMAT PARQUET)")
    (samples / 'temperatures-building.parquet').replace(parquet)
    parquet.chmod(0o644)

server = Path('/server-tls')
clients = Path('/client-tls')
server.mkdir(exist_ok=True)
clients.mkdir(exist_ok=True)


def openssl(*args):
    subprocess.run(['openssl', *map(str, args)], check=True, capture_output=True)


if not (server / 'ready').exists():
    # The CA private key never leaves this temporary container directory.
    with tempfile.TemporaryDirectory() as directory:
        ca = Path(directory)
        openssl('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '3650',
                '-subj', '/CN=DuckLake example CA', '-keyout', ca / 'ca.key', '-out', ca / 'ca.crt')
        shutil.copyfile(ca / 'ca.crt', server / 'ca.crt')
        shutil.copyfile(ca / 'ca.crt', clients / 'ca.crt')
        for name, cn, target, uid, usage in [
            ('server', 'postgres-tls', server, 999, 'serverAuth'),
            ('writer', 'ducklake', clients, 472, 'clientAuth'),
            ('reader', 'grafana_reader', clients, 472, 'clientAuth'),
        ]:
            openssl('req', '-new', '-newkey', 'rsa:2048', '-nodes', '-subj', f'/CN={cn}',
                    '-keyout', target / f'{name}.key', '-out', ca / f'{name}.csr')
            ext = ca / f'{name}.ext'
            ext.write_text(f'basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage={usage}\nsubjectAltName=DNS:{cn}\n')
            openssl('x509', '-req', '-in', ca / f'{name}.csr', '-CA', ca / 'ca.crt',
                    '-CAkey', ca / 'ca.key', '-CAserial', ca / 'ca.srl', '-CAcreateserial',
                    '-days', '3650', '-extfile', ext, '-out', target / f'{name}.crt')
            os.chown(target / f'{name}.key', uid, 0)
            (target / f'{name}.key').chmod(0o600)
    (server / 'pg_hba.conf').write_text('local all all trust\nhostssl all all all scram-sha-256 clientcert=verify-full\nhostnossl all all all reject\n')
    (server / 'ready').touch()
print('Read-only DuckDB, Parquet files, lake directories, and TLS certificates are ready.')
