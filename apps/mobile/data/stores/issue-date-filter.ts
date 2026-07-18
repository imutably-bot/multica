import {
  createRelativeIssueDateFilter,
  formatDateOnly,
  resolveIssueDateFilterRange,
  todayDateOnly,
} from "@multica/core/issues/date";

export type IssueDateField = "created_at" | "updated_at";
export type IssueDatePreset = "today" | "last_days";

export interface IssueDateFilter {
  field: IssueDateField;
  from: string;
  to: string;
  preset?: IssueDatePreset;
  days?: number;
}

export function createDefaultIssueDateFilter(
  field: IssueDateField = "created_at",
): IssueDateFilter {
  const today = todayDateOnly();
  return { field, from: today, to: today };
}

export function createMobileRelativeIssueDateFilter(
  field: IssueDateField,
  preset: IssueDatePreset,
  days?: number,
): IssueDateFilter {
  return createRelativeIssueDateFilter(field, preset, days);
}

export function formatIssueDateFilterLabel(filter: IssueDateFilter): string {
  const fieldLabel = filter.field === "created_at" ? "Created" : "Updated";
  if (filter.preset === "today") {
    return `${fieldLabel}: Today`;
  }
  if (filter.preset === "last_days") {
    const days = filter.days && filter.days > 0 ? filter.days : 3;
    return `${fieldLabel}: Last ${days} days`;
  }

  const resolved = resolveIssueDateFilterRange(filter);
  if (!resolved) return fieldLabel;
  const from = formatDateOnly(resolved.from);
  const to = formatDateOnly(resolved.to);
  if (!from || !to) return fieldLabel;
  return resolved.from === resolved.to
    ? `${fieldLabel}: ${from}`
    : `${fieldLabel}: ${from} - ${to}`;
}
