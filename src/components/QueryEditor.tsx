import React from "react";
import { QueryEditorProps } from "@grafana/data";
import { Button, CodeEditor, InlineField, Select, Stack } from "@grafana/ui";
import { DataSource } from "../datasource";
import { DEFAULT_QUERY, Options, Query } from "../types";

export function QueryEditor({
  query,
  onChange,
  onRunQuery,
}: QueryEditorProps<DataSource, Query, Options>) {
  return (
    <Stack direction="column" gap={2}>
      <CodeEditor
        language="sql"
        height={220}
        value={query.rawSql ?? DEFAULT_QUERY.rawSql}
        onChange={(rawSql) => onChange({ ...query, rawSql })}
        onSave={onRunQuery}
        showLineNumbers
      />
      <Stack direction="row" gap={2}>
        <InlineField label="Format">
          <Select
            aria-label="Query format"
            width={22}
            value={query.format ?? "table"}
            options={[
              { label: "Table", value: "table" },
              { label: "Time series", value: "time_series" },
            ]}
            onChange={(item) => {
              onChange({ ...query, format: item.value as Query["format"] });
              onRunQuery();
            }}
          />
        </InlineField>
        <Button onClick={onRunQuery} variant="secondary" icon="play">
          Run query
        </Button>
      </Stack>
      <details>
        <summary>SQL help</summary>
        <p>
          Queries run read-only. For time series, return a timestamp and numeric
          values, ordered by time. String columns become series labels.
        </p>
        <pre>{`SELECT $__timeGroupAlias(observed_at, '$__interval'),
       device, avg(value) AS value
FROM measurements
WHERE $__timeFilter(observed_at)
GROUP BY 1, 2
ORDER BY 1`}</pre>
        <p>
          Macros: <code>$__timeFilter(column)</code>, <code>$__timeFrom()</code>
          , <code>$__timeTo()</code>, <code>$__timeGroup(column, '5m')</code>,{" "}
          <code>$__interval</code>, <code>$__interval_ms</code>.
        </p>
        <p>
          Use <code>{"IN (${device:sqlstring})"}</code> for multi-value
          dashboard variables. Query <code>information_schema.tables</code> to
          inspect available tables.
        </p>
      </details>
    </Stack>
  );
}

export function VariableQueryEditor({
  query,
  onChange,
}: {
  query: string;
  onChange: (query: string, definition: string) => void;
}) {
  return (
    <CodeEditor
      language="sql"
      height={140}
      value={query ?? ""}
      onChange={(sql) => onChange(sql, sql)}
      showLineNumbers
    />
  );
}
