FROM python:3.13-slim-bookworm
RUN pip install --no-cache-dir duckdb==1.5.5
RUN mkdir -p /opt/duckdb/extensions && python -c "import duckdb; c=duckdb.connect(config={'extension_directory':'/opt/duckdb/extensions'}); c.execute('INSTALL ducklake; INSTALL postgres')"
COPY examples/ingest.py /app/ingest.py
CMD ["python", "-u", "/app/ingest.py"]
