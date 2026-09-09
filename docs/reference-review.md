# Reference review and implementation decisions

Reviewed on 2026-09-09. The reference repositories were read without modification:

- `grafana-duckdb-datasource`, commit `995271ad1f160594e20d2caa558a899667843c7a`.
- `grafanacon-custom-data-source-plugin-example`, commit `181ec9ef3eb7011cc71d3c3f7af15d97a0032a49`.

## Findings relevant to this plugin

1. **The DuckDB plugin's connection lifecycle needs to be separated from connection-local initialization.** In `pkg/plugin/duckdb_driver.go:158–231`, a global `Initialized` flag skips the initialization callback on subsequent pooled connections. Persistent objects are shared, but session state from setup SQL (such as a default schema or temporary view) is not necessarily shared. It also uses `context.Background()` for setup at line 168, losing caller cancellation. This implementation runs persistent setup in a transaction before exposing the pool, uses the initialization context, and selects the DuckLake catalog and UTC timezone on every query connection.
2. **Modification time is not a reliable indicator that a database file was replaced.** `pkg/plugin/datasource.go` uses file mtime to trigger reopening. Ordinary writes can change mtime; replacing a file while retaining or decreasing mtime can escape detection. Reopening also introduces lifetime coordination that is unnecessary for an embedded file. This implementation keeps each database open until its data source is disposed and documents restarting for snapshot replacement. Live ingestion uses DuckLake.
3. **Schema lookup in the DuckDB reference can mix columns from unrelated tables.** `src/datasource.ts:75–82` splits a qualified name by dots and then filters `information_schema.columns` only by the table name. Identical table names in different schemas/catalogs collide, and identifiers containing dots are mishandled. The initial implementation exposes a SQL editor and standard information-schema queries; it does not inherit this builder/completion logic.
4. **The DuckDB reference interpolates some setup values directly into SQL.** Environment paths and the MotherDuck token are concatenated into quoted SQL at `pkg/plugin/duckdb_driver.go:176–178,202`. Embedded quotes can break those statements. This implementation validates local paths, encodes engine configuration parameters, quotes SQL values, and uses a temporary PostgreSQL secret. PostgreSQL credentials stay in Grafana secure JSON. A real integration test uses a password containing both a quote and a backslash.
5. **The GrafanaCon example is a teaching scaffold, not a database implementation.** `pkg/plugin/datasource.go` hard-codes weather response fields and calls `http.Get` without a request context or timeout. It logs a URL containing the API token at lines 93–102 and in health checks, and its non-200 health branch returns before scheduling body closure. Its useful pattern is the SDK's instance factory, QueryData, CheckHealth, and disposal interfaces. This implementation uses those public interfaces with a local SQL backend and no copied weather/token logic.
6. **The reference packages contain broader UI/dependency scope than needed here.** The DuckDB reference depends on SQL builder internals, compatibility shims, and several `latest` package ranges. This plugin uses a small SQL editor over `DataSourceWithBackend`, pinned direct dependencies, its own plugin ID, and a generic database icon.

These are code-review findings, not a claim that the reference plugins were exhaustively security-tested. No implementation files or artwork were copied from either reference. The repository's existing MIT license remains in place.

## Documentation review

- [DuckDB Go client](https://duckdb.org/docs/current/clients/go): use the official `duckdb-go/v2` driver. The implementation pins `v2.10505.0`, embedding DuckDB 1.5.5. CGO requires native builds or a matching target toolchain.
- [DuckDB concurrency](https://duckdb.org/docs/current/connect/concurrency): a writable native file is owned by one process. Multiple independent read-only processes require no writer. This is why the product has separate existing-file and embedded-file modes.
- [DuckLake catalog selection](https://ducklake.select/docs/stable/duckdb/usage/choosing_a_catalog_database): PostgreSQL coordinates multiple clients, including remote clients. The catalog can run alongside Grafana or on a remote server; all readers and writers still share local storage.
- [PostgreSQL TLS](https://www.postgresql.org/docs/current/libpq-ssl.html): catalog connections support `disable`, `require`, `verify-ca`, and `verify-full`, defaulting to `verify-full`. Verification modes require an explicit CA file; optional client certificate/key paths support mutual TLS. The local demo explicitly disables TLS. File paths are escaped for libpq and then SQL; credentials remain in a temporary secret.
- [DuckLake connecting](https://ducklake.select/docs/stable/duckdb/usage/connecting): ATTACH can initialize a catalog and supports READ_ONLY and CREATE_IF_NOT_EXISTS. Bootstrap is optional, then Grafana uses a read-only attachment. Automatic migration is left disabled.
- [DuckLake storage](https://ducklake.select/docs/stable/duckdb/usage/choosing_storage): local directories are supported. This plugin requires an absolute local data path and sets OVERRIDE_DATA_PATH false rather than silently redirecting an existing catalog.
- [DuckLake access control](https://ducklake.select/docs/stable/duckdb/guides/access_control): catalog reader permissions and data-file access must both be provisioned. The local demo uses an owner to initialize the lake; an existing production lake should use a reader role.

## Query and lifecycle choices

The backend owns one database pool per Grafana data source instance. Settings changes and shutdown dispose the instance. A read/write lock prevents disposal during queries. Connections select UTC and the appropriate catalog before executing a read-only transaction. Embedded setup is restricted to administrator settings and runs transactionally at initialization.

The Go driver's `Conn.PrepareContext` and `QueryContext` execute prefixes of multi-statement input. Its `Conn.Prepare` rejects multiple statements without running their prefixes. The plugin uses that entry point to require one SELECT, then executes it with the request context. This also permits comments, CTEs, and semicolons in string literals without implementing another SQL parser. The driver's single-statement preparation phase itself has no context argument; cancellation is enforced before/after it and during execution. Binding operations may delay cancellation.

Grafana SDK frame conversion is extended for nullable small/unsigned integers, floats, decimals, huge integers, and nested values. Backend macros work for dashboards and alerts. Time series are converted to Grafana wide frames with labels; table queries retain row/column structure. Row caps warn on truncation, and query timeouts apply per query.

Read-only SQL is not a sandbox for untrusted SQL authors. DuckDB can read accessible local files and invoke table functions; OS permissions and mounted paths define access. Hosted providers, remote DuckDB services, cloud storage configuration, general extension management, streaming, and ingestion APIs are outside this plugin's scope.
