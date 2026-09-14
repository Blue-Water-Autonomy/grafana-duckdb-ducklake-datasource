import type { DataFrame, MetricFindValue } from "@grafana/data";

export function variableValues(frames: DataFrame[]): MetricFindValue[] {
  const values = new Map<string, MetricFindValue>();
  for (const frame of frames) {
    const text =
      frame.fields.find((field) => field.name === "__text") ?? frame.fields[0];
    const value =
      frame.fields.find((field) => field.name === "__value") ?? text;
    if (!text || !value) {
      continue;
    }
    for (let i = 0; i < frame.length; i++) {
      if (text.values[i] == null || value.values[i] == null) {
        continue;
      }
      const item = {
        text: String(text.values[i]),
        value: String(value.values[i]),
      };
      values.set(item.value, item);
    }
  }
  return [...values.values()];
}
