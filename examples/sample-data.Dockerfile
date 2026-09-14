FROM python:3.13-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends openssl && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir duckdb==1.5.5
RUN mkdir -p /opt/duckdb/extensions && python -c "import duckdb; c=duckdb.connect(config={'extension_directory':'/opt/duckdb/extensions'}); c.execute('INSTALL ducklake; INSTALL postgres')"
COPY examples/prepare.py examples/catalog.py examples/seed.py examples/ingest.py examples/parquet.py /app/
