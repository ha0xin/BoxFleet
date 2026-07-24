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
});
