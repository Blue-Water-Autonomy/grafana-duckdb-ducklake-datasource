import React from "react";
import { DataSourcePluginOptionsEditorProps } from "@grafana/data";
import {
  CodeEditor,
  Field,
  FieldSet,
  Input,
  SecretInput,
  Select,
  Switch,
} from "@grafana/ui";
import {
  ConnectionMode,
  Options,
  PostgresSSLMode,
  SecureOptions,
} from "../types";

export function ConfigEditor({
  options,
  onOptionsChange,
}: DataSourcePluginOptionsEditorProps<Options, SecureOptions>) {
  const config = options.jsonData;
  const mode =
    config.mode === "duckdb-managed"
      ? "duckdb-embedded"
      : config.mode || "duckdb-embedded";
  const sslMode = config.postgresSslMode ?? "verify-full";
  const set = <K extends keyof Options>(key: K, value: Options[K]) =>
    onOptionsChange({ ...options, jsonData: { ...config, [key]: value } });
  const textField = (
    key: keyof Options,
    label: string,
    placeholder: string,
    description?: string,
  ) => (
    <Field label={label} description={description}>
      <Input
        disabled={options.readOnly}
        aria-label={label}
        width={64}
        value={String(config[key] ?? "")}
        placeholder={placeholder}
        onChange={(event) => set(key, event.currentTarget.value)}
      />
    </Field>
  );
  const numberField = (
    key: keyof Options,
    label: string,
    fallback: number,
    min: number,
    max: number,
  ) => (
    <Field label={label}>
      <Input
        disabled={options.readOnly}
        aria-label={label}
        width={24}
        type="number"
        min={min}
        max={max}
        value={Number(config[key] ?? fallback)}
        onChange={(event) =>
          set(
            key,
            event.currentTarget.value === ""
              ? undefined
              : Number(event.currentTarget.value),
          )
        }
      />
    </Field>
  );
  return (
    <>
      <FieldSet label="Connection">
        <Field label="Database mode">
          <Select
            isDisabled={options.readOnly}
            aria-label="Database mode"
            width={64}
            value={mode}
            options={[
              {
                label: "Embedded DuckDB — file with setup SQL",
                value: "duckdb-embedded",
              },
              {
                label: "DuckLake — PostgreSQL catalog",
                value: "ducklake",
              },
            ]}
            onChange={(item) =>
              onOptionsChange({
                ...options,
                jsonData: {
                  ...config,
                  mode: item.value as ConnectionMode,
                  ...(item.value !== "duckdb-embedded"
                    ? { setupSql: "", setupSqlPath: "" }
                    : {}),
                },
              })
            }
          />
        </Field>
        {mode !== "ducklake" &&
          textField(
            "path",
            "Database file",
            "/var/lib/grafana/duckdb/analytics.duckdb",
            "Absolute path on the Grafana server. The database and parent directory are created if missing. Use views over local files for live data.",
          )}
        {mode === "duckdb-embedded" && (
          <>
            <Field
              label="Setup SQL"
              description="Runs when the data source initializes or settings change. Create views over local Parquet globs to query newly published files on each refresh."
            >
              <CodeEditor
                readOnly={options.readOnly}
                language="sql"
                height={220}
                value={config.setupSql ?? ""}
                onChange={(sql) => set("setupSql", sql)}
                showLineNumbers
              />
            </Field>
            {textField(
              "setupSqlPath",
              "Setup SQL file",
              "/etc/grafana/duckdb/setup.sql",
              "Optional alternative to inline SQL. Use one or the other; file changes take effect when the data source is reinitialized.",
            )}
          </>
        )}
        {mode === "ducklake" && (
          <>
            {textField(
              "postgresHost",
              "PostgreSQL host",
              "localhost",
              "Hostname or IP address of a local or remote PostgreSQL server. For hostname verification, it must match the server certificate.",
            )}
            {numberField("postgresPort", "PostgreSQL port", 5432, 1, 65535)}
            {textField(
              "postgresDatabase",
              "Catalog database",
              "ducklake",
              "The PostgreSQL database must already exist.",
            )}
            {textField("postgresUser", "PostgreSQL user", "grafana_reader")}
            <Field label="PostgreSQL password">
              <SecretInput
                disabled={options.readOnly}
                aria-label="PostgreSQL password"
                width={64}
                value={options.secureJsonData?.postgresPassword ?? ""}
                isConfigured={
                  options.secureJsonFields?.postgresPassword ?? false
                }
                onChange={(event) =>
                  onOptionsChange({
                    ...options,
                    secureJsonData: {
                      ...options.secureJsonData,
                      postgresPassword: event.currentTarget.value,
                    },
                  })
                }
                onReset={() =>
                  onOptionsChange({
                    ...options,
                    secureJsonFields: {
                      ...options.secureJsonFields,
                      postgresPassword: false,
                    },
                    secureJsonData: {
                      ...options.secureJsonData,
                      postgresPassword: "",
                    },
                  })
                }
              />
            </Field>
            <Field
              label="PostgreSQL TLS mode"
              description="Verify the certificate and hostname for remote catalogs. Disable TLS explicitly for a local server that does not support it."
            >
              <Select
                isDisabled={options.readOnly}
                aria-label="PostgreSQL TLS mode"
                width={64}
                value={sslMode}
                options={[
                  {
                    label: "Verify certificate and hostname (verify-full)",
                    value: "verify-full",
                  },
                  {
                    label: "Verify certificate authority (verify-ca)",
                    value: "verify-ca",
                  },
                  { label: "Require encryption (require)", value: "require" },
                  { label: "Disabled (disable)", value: "disable" },
                ]}
                onChange={(item) =>
                  onOptionsChange({
                    ...options,
                    jsonData: {
                      ...config,
                      postgresSslMode: item.value as PostgresSSLMode,
                      ...(item.value === "disable"
                        ? {
                            postgresSslRootCert: "",
                            postgresSslCert: "",
                            postgresSslKey: "",
                          }
                        : {}),
                    },
                  })
                }
              />
            </Field>
            {sslMode !== "disable" && (
              <>
                {textField(
                  "postgresSslRootCert",
                  "CA certificate file",
                  "/etc/grafana/postgres/ca.crt",
                  "Absolute path on the Grafana server to a trusted CA certificate bundle. Required for verify-ca and verify-full.",
                )}
                {textField(
                  "postgresSslCert",
                  "Client certificate file",
                  "/etc/grafana/postgres/client.crt",
                  "Optional, for servers that require client certificates. Configure the matching private key as well.",
                )}
                {textField(
                  "postgresSslKey",
                  "Client private key file",
                  "/etc/grafana/postgres/client.key",
                  "Absolute path to an unencrypted private key readable by Grafana. Restrict file permissions to the Grafana user.",
                )}
              </>
            )}
            {textField(
              "dataPath",
              "DuckLake data directory",
              "/var/lib/ducklake/data",
              "Use the same absolute directory in Grafana and the ingestion process. It must match the catalog’s stored data path. A remote catalog does not provide access to these files.",
            )}
            <Field
              label="Initialize DuckLake if missing"
              description="Requires catalog write permissions. Dashboard queries still use a read-only attachment. Leave disabled for an existing lake and a reader account."
            >
              <Switch
                disabled={options.readOnly}
                aria-label="Initialize DuckLake if missing"
                value={config.createDuckLake ?? false}
                onChange={(event) =>
                  set("createDuckLake", event.currentTarget.checked)
                }
              />
            </Field>
          </>
        )}
      </FieldSet>
      <FieldSet label="Query limits">
        {numberField("maxOpenConns", "Maximum connections", 4, 1, 32)}
        {numberField("maxRows", "Maximum returned rows", 100000, 1, 1000000)}
        {numberField("queryTimeout", "Query timeout (seconds)", 30, 1, 600)}
      </FieldSet>
      {mode === "ducklake" && (
        <FieldSet label="Extensions">
          {textField(
            "extensionDirectory",
            "Extension directory",
            "/var/lib/grafana/duckdb/extensions",
            "Optional local cache for the DuckLake and PostgreSQL extensions. Preinstall matching extensions here for offline deployments.",
          )}
        </FieldSet>
      )}
    </>
  );
}
