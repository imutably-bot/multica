import { formatDateOnly, todayDateOnly } from "@multica/core/issues/date";

export type IssueDateField = "created_at" | "updated_at";

export interface IssueDateFilter {
  field: IssueDateField;
  from: string;
  to: string;
}

export function createDefaultIssueDateFilter(
  field: IssueDateField = "created_at",
): IssueDateFilter {
  const today = todayDateOnly();
  return { field, from: today, to: today };
}

export function formatIssueDateFilterLabel(filter: IssueDateFilter): string {
  const fieldLabel = filter.field === "created_at" ? "Created" : "Updated";
  const from = formatDateOnly(filter.from);
  const to = formatDateOnly(filter.to);
  if (!from || !to) return fieldLabel;
  return filter.from === filter.to
    ? `${fieldLabel}: ${from}`
    : `${fieldLabel}: ${from} - ${to}`;
}
