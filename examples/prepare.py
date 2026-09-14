"""Seed live Parquet data and prepare demo TLS certificates before readers start."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile

from parquet import DIRECTORY, seed


samples = Path('/samples')
samples.mkdir(exist_ok=True)
for name in ['local', 'tls']:
    directory = Path('/var/lib/ducklake') / name
    directory.mkdir(parents=True, exist_ok=True)
    os.chown(directory, 472, 0)
    directory.chmod(0o750)

DIRECTORY.mkdir(exist_ok=True)
os.chown(DIRECTORY, 472, 0)
DIRECTORY.chmod(0o755)
seed()

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
print('Live Parquet data, lake directories, and TLS certificates are ready.')
