// formatArchiveDate renders an archived session's date as DD/MM/YY for the
// drawer list and the read-only banner. Returns "" for missing/invalid input.
export function formatArchiveDate(iso: string | null | undefined): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleDateString("en-GB", { day: "2-digit", month: "2-digit", year: "2-digit" });
}
