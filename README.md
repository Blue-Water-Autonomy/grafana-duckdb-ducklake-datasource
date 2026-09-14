# DuckDB / DuckLake for Grafana

A Grafana data source for live data alongside your Grafana instance. The plugin embeds DuckDB 1.5.5 in its Go backend and presents standard Grafana configuration and SQL editors. No hosted account is required.

## Connection modes

| Mode | Use | Lifecycle |
| --- | --- | --- |
| Embedded DuckDB | Query newly published local files through persistent views | Creates missing parent directories and the database, then runs administrator setup SQL. |
| DuckLake | Query while another process ingests data | Uses a local or remote PostgreSQL catalog and a local Parquet directory; attaches the lake read-only. |

Paths are **absolute paths inside the Grafana server/container**, not the browser. Mount files and directories accordingly. Use one embedded data source per DuckDB file.

Native DuckDB files cannot have a writer process alongside independent reader processes. Embedded mode gives the plugin ownership of its database. Embedded views can read new, complete Parquet files published by another process. Use DuckLake for concurrent database ingestion. See [DuckDB concurrency](https://duckdb.org/docs/current/connect/concurrency).

## Run the complete example

The [examples directory](examples/README.md) contains a runnable stack with four configured data sources, sample data, continuous ingestion, and a provisioned dashboard. It covers embedded DuckDB with inline SQL or a setup file over live Parquet data, and DuckLake with both local and verified TLS PostgreSQL catalogs.

Requires Docker Compose 2.20 or newer. From the repository root:

```sh
docker compose up --build -d --wait
```

Open [the example dashboard](http://localhost:3000/d/duckdb-ducklake-examples) and log in with `admin` / `admin`. The same command works from `examples/`. Every source has data before Grafana starts, and every source receives new measurements every five seconds. PostgreSQL stays on the private Compose network; data remains in local volumes.

See [examples/README.md](examples/README.md) for the data-source matrix, configuration, alternate ports, and lifecycle commands. `docker compose down` stops the stack and preserves the example volumes.

## Embedded database and setup script

Select **Embedded DuckDB — file with setup SQL**. Supply a database path and either inline **Setup SQL** or an absolute **Setup SQL file** path:

```sql
CREATE OR REPLACE VIEW measurements AS
SELECT * FROM read_parquet('/var/lib/telemetry/*.parquet', union_by_name = true);
```

Setup runs in a transaction once per data source initialization, including after a restart or settings change. Use idempotent persistent DDL such as `CREATE OR REPLACE VIEW` and `CREATE TABLE IF NOT EXISTS`. Avoid temporary objects and connection-specific state: dashboard queries use a pool of connections. Do not include transaction-control statements (`BEGIN`, `COMMIT`, `ROLLBACK`); the plugin controls the transaction. Changing a setup file on disk takes effect after restarting Grafana or updating the data source configuration.

Each dashboard query discovers new files matching the view's glob, without rerunning setup or reopening the database. Setup creates the view; the external ingestion process publishes the data. Globs must match valid files when DuckDB binds the view. Publish complete files atomically into the directory. Grafana needs read access to those files and write access to the embedded database directory. A failed setup fails initialization and rolls back its database changes; it does not remove a newly created empty database file.

## DuckLake with a PostgreSQL catalog

Use an existing PostgreSQL database on the same host, alongside Grafana, or on a remote server, and a local Parquet directory. Enter the host, port, database, user, secure password, TLS settings, and data directory. Remote catalogs support the same concurrent ingestion workflow. PostgreSQL provides the catalog connection; Grafana and every ingestion process still need access to the same Parquet files. See [DuckLake catalog selection](https://ducklake.select/docs/stable/duckdb/usage/choosing_a_catalog_database).

**PostgreSQL TLS mode** defaults to `verify-full`, including when omitted from provisioning. It requires a **CA certificate file** and checks that the configured hostname or IP matches the server certificate. The local Compose example explicitly sets `postgresSslMode: disable`. Existing local configurations that omit this field should add `disable` or configure TLS.

| TLS mode | Behavior |
| --- | --- |
| `verify-full` (default) | Requires encryption and verifies the certificate chain and hostname. |
| `verify-ca` | Requires encryption and verifies the certificate chain, without checking the hostname. |
| `require` | Requires encryption; use a verification mode when server identity must be checked. |
| `disable` | Plaintext connection for a local server configured without TLS. |

Certificate settings are absolute paths **inside the Grafana server/container**. Mount a trusted CA bundle and configure `postgresSslRootCert` for either verification mode. For mutual TLS, also configure `postgresSslCert` and `postgresSslKey` together. Use an unencrypted client key readable by the Grafana process with restrictive permissions (for example, owned by Grafana with mode `0600`). Only paths are saved in configuration; private key contents stay on the filesystem. Passwords remain in Grafana's secure settings. See [PostgreSQL TLS verification and client certificates](https://www.postgresql.org/docs/current/libpq-ssl.html).

The [remote catalog provisioning example](examples/remote-ducklake.yaml.example) uses `verify-full` and local storage. Configure the same catalog and suitable TLS options independently in your ingestion client.

For an existing lake, leave **Initialize DuckLake if missing** disabled and use a catalog reader account. To bootstrap a lake, enable it with an account that can create the catalog tables. The PostgreSQL database itself must already exist. After bootstrap, change to a reader account and disable initialization. The plugin does not create ingestion tables or manage ingestion.

Data paths must match the path recorded in the catalog. Mount the same directory at the **same absolute path** for Grafana and every writer. The plugin explicitly disables data-path override and automatic schema migration. Follow [DuckLake connecting](https://ducklake.select/docs/stable/duckdb/usage/connecting) and [access control](https://ducklake.select/docs/stable/duckdb/guides/access_control) when provisioning an existing catalog.

Outside the supplied Docker image, DuckLake first tries `LOAD ducklake` and `LOAD postgres`, then installs a missing extension from DuckDB's official repository. Preinstall them with the same platform and engine version for offline use:

```sh
go run ./cmd/install-extensions /absolute/path/to/extensions
```

Set **Extension directory** to that directory. It defaults to `$GF_PATHS_DATA/duckdb/extensions` when Grafana defines that environment variable, otherwise DuckDB's default cache. The Docker image uses `/opt/duckdb/extensions`.

## Queries and variables

Dashboard queries accept a single SELECT statement (including CTEs and DuckDB's FROM-first syntax) and run in a read-only transaction. Initialization SQL is administrator-controlled and separate. Read-only execution is not a filesystem sandbox: trusted query authors can read files accessible to the Grafana process, including through table functions. Use OS permissions and container mounts to control that access. This plugin has no hosted database, object-storage credential, or remote DuckDB configuration.

| Macro | Result |
| --- | --- |
| `$__timeFilter(column)` | Inclusive UTC range filter |
| `$__timeFrom()` / `$__timeTo()` | UTC timestamp literals retaining fractional seconds |
| `$__timeGroup(column, '5m')` | DuckDB `time_bucket` expression |
| `$__timeGroupAlias(column, '$__interval')` | Same expression with alias `time` |
| `$__interval` / `$__interval_ms` | Grafana query interval |

Time series need a timestamp column, numeric values, and ascending time order. String columns become labels. Decimal values become floating point numbers for charts; cast decimals to VARCHAR to preserve exact textual precision. HUGEINT/UHUGEINT/BIGNUM become strings. Nested values become JSON text and blobs become base64. SQL NULL remains null.

Query variables use the first result column, or `__text` and `__value` aliases:

```sql
SELECT device_name AS __text, device_id AS __value FROM devices
```

Reference multi-value variables with `IN (${device:sqlstring})`. Values use SQL string escaping by default. Macros run in the backend and also work for alert queries; dashboard variables are a frontend feature and should not be used in alert rules.

Defaults: 4 connections, 100,000 returned rows, 30-second query timeout. The row cap limits frame size and reports a warning on truncation; it does not bound DuckDB's execution memory. Use SQL aggregation/LIMIT and appropriate process/container resources for large datasets. Save & test verifies the database/catalog connection, not every external file referenced by a view.

## Develop and verify

Requires Go 1.24+, a C/C++ toolchain (CGO), and Node.js 22+. The lockfile is generated with npm 10.9.8, also used in the container build.

```sh
npm ci
make build
make test
make check
make integration
```

The integration tests use disposable PostgreSQL containers and temporary certificates (requires `openssl`). They verify concurrent ingestion over plaintext and verified mutual TLS, encrypted catalog sessions, rejection of an untrusted CA or mismatched hostname, and failure when TLS is required but unavailable. Unit tests exercise the real embedded engine, including creation, rejection of dashboard writes, setup rollback, live Parquet views, nullable types, time macros, labels, row limits, and cancellation.

To run browser tests against the local example:

```sh
docker compose up --build -d --wait
npx playwright install chromium
npm run e2e
```

These tests verify all four example data sources, embedded setup through the UI, table and time-series queries in Explore, concurrent ingestion, and the provisioned dashboard with its device filter. Screenshots are written under `test-results/`.

`make build` creates `dist/module.js`, `dist/plugin.json`, assets and a backend binary for the current platform. For Linux deployment from macOS, use the Docker build; CGO requires a compiler for the target platform. Install the complete `dist` directory under Grafana's plugin directory and allow the unsigned plugin ID `local-duckdb-datasource` in your local Grafana configuration. No signing or publishing is performed by this repository.

The reference review and design decisions are recorded in [docs/reference-review.md](docs/reference-review.md).
