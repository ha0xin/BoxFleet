import { describe, expect, it } from "vitest";

import { adminKeys, queryString } from "./query";

describe("queryString", () => {
  it("omits undefined and empty values while retaining false and zero", () => {
    expect(queryString({ search: "", deleted: false, offset: 0, limit: 25, cursor: undefined }))
      .toBe("?deleted=false&offset=0&limit=25");
  });

  it("encodes user input", () => {
    expect(queryString({ search: "a & b" })).toBe("?search=a+%26+b");
  });
});

describe("adminKeys", () => {
  it("keeps list variants under the same admin root", () => {
    expect(adminKeys.users(false)).toEqual(["admin", "users", false]);
    expect(adminKeys.users(true)).toEqual(["admin", "users", true]);
  });

  it("separates the telemetry series caches from the network-events table cache", () => {
    const filters = { range: "24h", bucket: "hour" };
    const keys = [
      adminKeys.networkEvents(filters),
      adminKeys.networkEventSeries(filters),
      adminKeys.networkEventServices(filters),
      adminKeys.networkEventHosts(filters),
      adminKeys.trafficSeries(filters)
    ];
    expect(new Set(keys.map((key) => key[1])).size).toBe(keys.length);
    expect(keys.every((key) => key[0] === "admin")).toBe(true);
    expect(adminKeys.trafficSeries(filters)).toEqual(["admin", "traffic-series", filters]);
  });
});
