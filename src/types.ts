import type { DataSourceJsonData } from "@grafana/data";
import type { DataQuery } from "@grafana/schema";

export type ConnectionMode = "duckdb-embedded" | "duckdb-managed" | "ducklake";
export type PostgresSSLMode =
  | "disable"
  | "require"
  | "verify-ca"
  | "verify-full";

export interface Options extends DataSourceJsonData {
  mode?: ConnectionMode;
  path?: string;
  setupSql?: string;
  setupSqlPath?: string;
  dataPath?: string;
  postgresHost?: string;
  postgresPort?: number;
  postgresDatabase?: string;
  postgresUser?: string;
  postgresSslMode?: PostgresSSLMode;
  postgresSslRootCert?: string;
  postgresSslCert?: string;
  postgresSslKey?: string;
  createDuckLake?: boolean;
  extensionDirectory?: string;
  maxOpenConns?: number;
  maxRows?: number;
  queryTimeout?: number;
}

export interface SecureOptions {
  postgresPassword?: string;
}

export interface Query extends DataQuery {
  rawSql: string;
  format: "table" | "time_series";
}

export const DEFAULT_QUERY = {
  rawSql: "SELECT now() AS time, 1.0 AS value",
  format: "table" as const,
};
