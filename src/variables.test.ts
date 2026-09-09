import { describe, expect, it } from "vitest";
import type { DataFrame } from "@grafana/data";
import { variableValues } from "./variables";

function frame(columns: Record<string, unknown[]>): DataFrame {
  return {
    name: "variables",
    length: Object.values(columns)[0]?.length ?? 0,
    fields: Object.entries(columns).map(([name, values]) => ({
      name,
      values,
      type: "string",
      config: {},
    })),
  } as DataFrame;
}

describe("dashboard query variables", () => {
  it("keeps distinct IDs with the same display name and removes duplicate values", () => {
    expect(
      variableValues([
        frame({ __text: ["Pump", "Pump", "Pump"], __value: [1, 2, 1] }),
      ]),
    ).toEqual([
      { text: "Pump", value: "1" },
      { text: "Pump", value: "2" },
    ]);
  });
  it("uses the first column when aliases are absent and skips nulls", () => {
    expect(
      variableValues([
        frame({ device: ["a", null, "b"], ignored: [10, 20, 30] }),
      ]),
    ).toEqual([
      { text: "a", value: "a" },
      { text: "b", value: "b" },
    ]);
  });
  it("handles empty results", () => {
    expect(variableValues([])).toEqual([]);
  });
});
