import { describe, expect, it } from "vitest";
import { formatArchiveDate } from "./archiveDate";

describe("formatArchiveDate", () => {
  it("formats a date as DD/MM/YY", () => {
    expect(formatArchiveDate("2026-02-28T12:00:00")).toBe("28/02/26");
  });

  it("returns empty for missing or invalid input", () => {
    expect(formatArchiveDate(null)).toBe("");
    expect(formatArchiveDate(undefined)).toBe("");
    expect(formatArchiveDate("not-a-date")).toBe("");
  });
});
