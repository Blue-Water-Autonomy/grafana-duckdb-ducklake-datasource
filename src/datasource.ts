import {
  CoreApp,
  DataSourceInstanceSettings,
  ScopedVars,
  TimeRange,
  MetricFindValue,
  dateTime,
} from "@grafana/data";
import { DataSourceWithBackend, getTemplateSrv } from "@grafana/runtime";
import { lastValueFrom } from "rxjs";
import { Options, Query, DEFAULT_QUERY } from "./types";
import { variableValues } from "./variables";

export class DataSource extends DataSourceWithBackend<Query, Options> {
  constructor(settings: DataSourceInstanceSettings<Options>) {
    super(settings);
  }

  getDefaultQuery(_: CoreApp): Partial<Query> {
    return DEFAULT_QUERY;
  }

  filterQuery(query: Query): boolean {
    return !query.hide && !!query.rawSql?.trim();
  }

  applyTemplateVariables(query: Query, scopedVars: ScopedVars): Query {
    return {
      ...query,
      rawSql: getTemplateSrv().replace(query.rawSql, scopedVars, "sqlstring"),
    };
  }

  async metricFindQuery(
    rawSql: string,
    options?: { range?: TimeRange; scopedVars?: ScopedVars },
  ): Promise<MetricFindValue[]> {
    const range = options?.range ?? {
      from: dateTime().subtract(6, "hours"),
      to: dateTime(),
      raw: { from: "now-6h", to: "now" },
    };
    const response = await lastValueFrom(
      this.query({
        requestId: `variable-${crypto.randomUUID()}`,
        app: CoreApp.Dashboard,
        range,
        timezone: "utc",
        interval: "1m",
        intervalMs: 60000,
        scopedVars: options?.scopedVars ?? {},
        targets: [{ refId: "variable", rawSql, format: "table" }],
        startTime: Date.now(),
      }),
    );
    if (response.errors?.length) {
      throw new Error(response.errors.map((error) => error.message).join("; "));
    }
    return variableValues(response.data);
  }
}
