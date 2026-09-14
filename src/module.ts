import { DataSourcePlugin } from "@grafana/data";
import { DataSource } from "./datasource";
import { ConfigEditor } from "./components/ConfigEditor";
import { QueryEditor, VariableQueryEditor } from "./components/QueryEditor";
import { Options, Query } from "./types";

export const plugin = new DataSourcePlugin<DataSource, Query, Options>(
  DataSource,
)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor)
  .setVariableQueryEditor(VariableQueryEditor);
