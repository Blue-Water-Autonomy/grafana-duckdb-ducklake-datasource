"""Test concurrent ingestion and TLS against disposable PostgreSQL catalogs."""
import os
from pathlib import Path
import subprocess
import tempfile
import time


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, **kwargs)


def openssl(*args):
    run("openssl", *map(str, args), capture_output=True)


def make_certificates(root):
    # Exercise both libpq and SQL quoting with real certificate filenames.
    client = root / "client 'quoted\\ paths"
    client.mkdir()
    openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
            "-subj", "/CN=DuckLake test CA", "-keyout", root / "ca.key", "-out", root / "ca.crt")
    for name, cn, usage, san in [("server", "localhost", "serverAuth", "DNS:localhost"),
                                 ("client", "test", "clientAuth", "DNS:test")]:
        openssl("req", "-new", "-newkey", "rsa:2048", "-nodes", "-subj", f"/CN={cn}",
                "-keyout", root / f"{name}.key", "-out", root / f"{name}.csr")
        ext = root / f"{name}.ext"
        ext.write_text(f"basicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage={usage}\nsubjectAltName={san}\n")
        openssl("x509", "-req", "-in", root / f"{name}.csr", "-CA", root / "ca.crt",
                "-CAkey", root / "ca.key", "-CAserial", root / "ca.srl", "-CAcreateserial", "-days", "1", "-extfile", ext,
                "-out", root / f"{name}.crt")
    openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
            "-subj", "/CN=Untrusted CA", "-keyout", root / "untrusted.key", "-out", client / "untrusted.crt")
    for name in ["ca.crt", "client.crt", "client.key"]:
        (client / name).write_bytes((root / name).read_bytes())
    (client / "client.key").chmod(0o600)
    (root / "pg_hba.conf").write_text("local all all trust\nhostssl all all all scram-sha-256 clientcert=verify-full\nhostnossl all all all reject\n")
    return client


def test_catalog(cert_root=None, client_certs=None):
    password = "test-only-'quoted\\password"
    args = ["docker", "run", "--rm", "-d", "--tmpfs", "/var/lib/postgresql/data",
            "-e", "POSTGRES_DB=test", "-e", "POSTGRES_USER=test", "-e", f"POSTGRES_PASSWORD={password}",
            "-p", "127.0.0.1::5432"]
    if cert_root:
        args += ["-v", f"{cert_root}:/input-tls:ro", "--entrypoint", "/bin/bash", "postgres:17-bookworm", "-ceu",
                 "mkdir -p /tmp/tls; cp /input-tls/{ca.crt,server.crt,server.key,pg_hba.conf} /tmp/tls/; "
                 "chown -R postgres:postgres /tmp/tls; chmod 600 /tmp/tls/server.key; "
                 "exec /usr/local/bin/docker-entrypoint.sh postgres -c ssl=on "
                 "-c ssl_cert_file=/tmp/tls/server.crt -c ssl_key_file=/tmp/tls/server.key "
                 "-c ssl_ca_file=/tmp/tls/ca.crt -c hba_file=/tmp/tls/pg_hba.conf"]
    else:
        args += ["postgres:17-bookworm"]
    container = run(*args, capture_output=True).stdout.strip()
    storage = tempfile.TemporaryDirectory(prefix="ducklake-data-")
    try:
        for _ in range(60):
            ready = subprocess.run(["docker", "exec", container, "pg_isready", "-U", "test", "-d", "test"], capture_output=True)
            if ready.returncode == 0:
                break
            time.sleep(1)
        else:
            run("docker", "logs", container)
            raise RuntimeError("PostgreSQL did not become ready")
        port = run("docker", "port", container, "5432", capture_output=True).stdout.strip().rsplit(":", 1)[1]
        env = {**os.environ, "DUCKLAKE_TEST_PORT": port, "DUCKLAKE_TEST_PASSWORD": password,
               "DUCKLAKE_TEST_DATA": storage.name,
               "DUCKLAKE_TEST_CERTS": str(client_certs) if client_certs else ""}
        print("Testing verified mutual TLS" if cert_root else "Testing local plaintext and TLS enforcement", flush=True)
        run("go", "test", "./pkg/plugin", "-run", "^TestDuckLake(ConcurrentProcess|TLS)$", "-count=1", "-v", env=env)
    finally:
        run("docker", "stop", container, stdout=subprocess.DEVNULL)
        storage.cleanup()


test_catalog()
# Keep bind mounts inside the workspace, which is shared by Docker Desktop/Colima.
scratch = Path.cwd() / ".local"
scratch.mkdir(exist_ok=True)
with tempfile.TemporaryDirectory(prefix="ducklake-tls-", dir=scratch) as directory:
    root = Path(directory)
    test_catalog(root, make_certificates(root))
