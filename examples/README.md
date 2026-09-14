# DuckDB / DuckLake examples

A complete Grafana stack with four provisioned data sources, continuous ingestion for every source, and a dashboard that queries all four. Requires Docker with Compose 2.20 or newer. No cloud account, host Python, DuckDB installation, or manual data-source configuration is needed.

From this directory:

```sh
docker compose up --build -d --wait
```

Open [the example dashboard](http://localhost:3000/d/duckdb-ducklake-examples) and log in with **admin / admin**. It is also Grafana's default home dashboard and appears in the **Examples** folder. The first build downloads the plugin's dependencies and matching DuckDB extensions.

The repository root's `compose.yaml` includes this configuration, so the same command also works from the repository root. Both commands use the `duckdb-ducklake-examples` project.

## Included data sources

| Grafana data source | Configuration | Sample data |
| --- | --- | --- |
| Local DuckDB | `duckdb-embedded`; inline setup SQL | Grafana creates its own database and defines `example_metrics` over live Parquet batches. |
| Embedded DuckDB (Parquet setup file) | `duckdb-embedded`; [setup.sql](setup.sql) | Grafana creates a separate database and a view over `/samples/measurements/*.parquet`. |
| Local DuckLake | `ducklake`; PostgreSQL without TLS; initialization enabled | A seeded `measurements` table and continuous ingestion into `/var/lib/ducklake/local`. |
| DuckLake (TLS PostgreSQL catalog) | `ducklake`; `verify-full`, client certificate, reader role; initialization disabled | An existing catalog, seeded history, and continuous ingestion into `/var/lib/ducklake/tls`. |

The TLS PostgreSQL service simulates a remote catalog over TCP while keeping the entire example on your machine. Server hostname verification and mutual TLS are enabled. Its Grafana data source uses `grafana_reader`, which has catalog read permissions; the seed and ingestion services use a separate writer account. PostgreSQL ports are private to the Compose network. Both lakes keep their data in local Docker volumes.

All examples report temperatures for `sensor-1`, `sensor-2`, and `sensor-3`. Every source starts with two hours of history and receives three fresh measurements every five seconds, with timestamps recorded at ingestion. Both embedded examples read the same stream of immutable Parquet batches through a glob; the writer closes each staging file before atomically publishing it. Both DuckLake catalogs receive the same live measurements. The lakes retain six hours of logical history. This demo preserves published Parquet batches and does not schedule physical DuckLake file cleanup, so disk usage grows until the example volumes are reset.

## Dashboard

[overview.json](dashboards/overview.json) is provisioned automatically. It includes four sample-count cards, a time-series chart for each data source, and a table of the latest ingested TLS-catalog measurements. The **Device** query variable filters every panel. The dashboard refreshes every five seconds.

Panel SQL demonstrates `$__timeFilter`, `$__timeGroupAlias`, `$__interval`, and `${device:sqlstring}`. View or edit panels to inspect the queries. Save a copy to experiment; the provisioned original comes from the JSON file. File edits are picked up within 30 seconds.

## Configuration and lifecycle

- [compose.yaml](compose.yaml) defines Grafana, two PostgreSQL catalogs, file preparation, catalog seeding, and continuous ingestion.
- [provisioning/datasources/local.yaml](provisioning/datasources/local.yaml) defines all four plugin instances.
- [prepare.py](prepare.py) prepares the Parquet directory, backfills history if the last 30 minutes are empty, and creates demo certificates. Published files are preserved across restarts.
- [seed.py](seed.py) initializes both lakes, backfills data if the last 30 minutes are empty, and grants the TLS reader's permissions.
- [ingest.py](ingest.py) publishes new Parquet batches and writes to both lakes as a separate process.
- [parquet.py](parquet.py) generates timestamped measurements and atomically publishes complete files for both embedded views.

The generated certificates are for this local demonstration. Certificate private keys stay in Docker volumes; the temporary CA private key is discarded. The Grafana and ingestion processes access data at the same absolute paths. Stopping the ingestion service leaves all data sources queryable with their recorded timestamps; the dashboard will stop receiving fresh measurements:

```sh
docker compose stop ingest
docker compose start ingest
```

Override the port or demonstration passwords with environment variables:

```sh
GRAFANA_PORT=3001 GRAFANA_ADMIN_PASSWORD=my-password docker compose up --build -d --wait
```

Available variables are `GRAFANA_PORT` (3000), `GRAFANA_ADMIN_PASSWORD` (admin), `DUCKLAKE_PASSWORD` (local-development), and `DUCKLAKE_READER_PASSWORD` (example-reader). Use the same environment values on subsequent Compose commands. Grafana's administrator password and PostgreSQL's writer password initialize new volumes only; changing those variables does not rotate existing database accounts.

Stop the stack while retaining its databases and certificates:

```sh
docker compose down
```

To intentionally discard all example data, dashboards saved in Grafana, and demo certificates, stop it with `docker compose down -v`, then start it again. Volumes belonging to other Compose projects are unaffected.

For development verification, run `npm run e2e` from the repository root after starting this stack. Use `GRAFANA_URL=http://localhost:3001` when testing an alternate port.
