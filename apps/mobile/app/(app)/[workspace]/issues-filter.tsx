/**
 * Status + Priority + Date filter sheet — presented as a formSheet by the
 * parent Stack. Shared by My Issues and the workspace-wide Issues page;
 * which view-store to read/write is selected by the `scope` URL param.
 *
 * Routes that open this sheet:
 *   - /[workspace]/issues-filter?scope=my   →  useMyIssuesViewStore
 *   - /[workspace]/issues-filter?scope=all  →  useIssuesViewStore
 *
 * Self-contained: reads/writes the store directly, no callback passing.
 */
import { Pressable, ScrollView, View } from "react-native";
import DateTimePicker from "@react-native-community/datetimepicker";
import { useLocalSearchParams } from "expo-router";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { dateOnlyToLocalDate, toDateOnly } from "@multica/core/issues/date";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { useIssuesViewStore } from "@/data/stores/issues-view-store";
import {
  createDefaultIssueDateFilter,
  type IssueDateField,
  type IssueDateFilter,
} from "@/data/stores/issue-date-filter";
import { useMyIssuesViewStore } from "@/data/stores/my-issues-view-store";
import { BOARD_STATUSES, STATUS_LABEL } from "@/lib/issue-status";
import { cn } from "@/lib/utils";

const ALL_STATUSES: IssueStatus[] = [...BOARD_STATUSES, "cancelled"];

// Mirrors PRIORITY_ORDER in packages/core/issues/config/priority.ts.
const PRIORITY_ORDER: IssuePriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

// Label map duplicated across several mobile files — out of scope to
// consolidate per the SheetShell migration plan.
const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
  none: "No priority",
};

const DATE_FIELD_OPTIONS: { value: IssueDateField; label: string }[] = [
  { value: "created_at", label: "Created date" },
  { value: "updated_at", label: "Updated date" },
];

type Scope = "my" | "all";

export default function IssuesFilterRoute() {
  const { scope } = useLocalSearchParams<{ scope?: string }>();
  const resolvedScope: Scope = scope === "all" ? "all" : "my";

  const statusFilters = useScopedFilters(resolvedScope, "status");
  const priorityFilters = useScopedFilters(resolvedScope, "priority");
  const allDateFilter = useIssuesViewStore((s) => s.dateFilter);
  const myDateFilter = useMyIssuesViewStore((s) => s.dateFilter);
  const dateFilter = resolvedScope === "all" ? allDateFilter : myDateFilter;

  const onToggleStatus = (s: IssueStatus) => {
    if (resolvedScope === "all") {
      useIssuesViewStore.getState().toggleStatusFilter(s);
    } else {
      useMyIssuesViewStore.getState().toggleStatusFilter(s);
    }
  };
  const onTogglePriority = (p: IssuePriority) => {
    if (resolvedScope === "all") {
      useIssuesViewStore.getState().togglePriorityFilter(p);
    } else {
      useMyIssuesViewStore.getState().togglePriorityFilter(p);
    }
  };
  const setDateFilter = (next: IssueDateFilter | null) => {
    if (resolvedScope === "all") {
      useIssuesViewStore.getState().setDateFilter(next);
    } else {
      useMyIssuesViewStore.getState().setDateFilter(next);
    }
  };
  const onClearFilters = () => {
    if (resolvedScope === "all") {
      useIssuesViewStore.getState().clearFilters();
    } else {
      useMyIssuesViewStore.getState().clearFilters();
    }
  };

  const currentDateFilter = dateFilter ?? createDefaultIssueDateFilter();
  const hasActive =
    statusFilters.length > 0 || priorityFilters.length > 0 || !!dateFilter;

  const onChangeDateField = (field: IssueDateField) => {
    setDateFilter({ ...currentDateFilter, field });
  };
  const onChangeDateFrom = (selected: Date | undefined) => {
    if (!selected) return;
    setDateFilter(
      normalizeDateFilter({
        ...currentDateFilter,
        from: toDateOnly(selected),
      }),
    );
  };
  const onChangeDateTo = (selected: Date | undefined) => {
    if (!selected) return;
    setDateFilter(
      normalizeDateFilter({
        ...currentDateFilter,
        to: toDateOnly(selected),
      }),
    );
  };

  return (
    <View className="flex-1">
      <View className="flex-row items-center justify-between px-4 pt-4 pb-3">
        <Text className="text-base font-semibold text-foreground">Filter</Text>
        {hasActive ? (
          <Pressable
            onPress={onClearFilters}
            hitSlop={8}
            className="px-2 py-1 active:opacity-60"
          >
            <Text className="text-sm text-primary font-medium">Reset</Text>
          </Pressable>
        ) : null}
      </View>
      <ScrollView className="flex-1" showsVerticalScrollIndicator={false}>
        <SectionLabel>Date</SectionLabel>
        <View className="gap-3 px-4 pb-4">
          <RadioGroup
            value={currentDateFilter.field}
            onValueChange={(value) =>
              onChangeDateField(value as IssueDateField)
            }
            className="gap-0 rounded-xl border border-border overflow-hidden"
          >
            {DATE_FIELD_OPTIONS.map((option, idx) => {
              const isLast = idx === DATE_FIELD_OPTIONS.length - 1;
              return (
                <View key={option.value}>
                  <Pressable
                    onPress={() => onChangeDateField(option.value)}
                    className="flex-row items-center gap-3 px-4 py-3.5 active:bg-secondary"
                  >
                    <RadioGroupItem value={option.value} />
                    <Text className="flex-1 text-sm font-medium text-foreground">
                      {option.label}
                    </Text>
                  </Pressable>
                  {!isLast ? <View className="h-px bg-border ml-11" /> : null}
                </View>
              );
            })}
          </RadioGroup>

          <DateRangeCard
            label="From"
            value={currentDateFilter.from}
            onChange={onChangeDateFrom}
            maximumDate={dateOnlyToLocalDate(currentDateFilter.to)}
          />
          <DateRangeCard
            label="To"
            value={currentDateFilter.to}
            onChange={onChangeDateTo}
            minimumDate={dateOnlyToLocalDate(currentDateFilter.from)}
          />

          {!dateFilter ? (
            <Text className="px-1 text-xs text-muted-foreground">
              Adjust a date to activate the filter.
            </Text>
          ) : null}
        </View>

        <SectionLabel>Status</SectionLabel>
        {ALL_STATUSES.map((status) => {
          const checked = statusFilters.includes(status);
          return (
            <Pressable
              key={status}
              onPress={() => onToggleStatus(status)}
              className={cn(
                "flex-row items-center gap-3 px-4 py-2.5 active:bg-secondary",
                checked && "bg-secondary/60",
              )}
            >
              <StatusIcon status={status} size={16} />
              <Text className="flex-1 text-sm text-foreground">
                {STATUS_LABEL[status]}
              </Text>
              <CheckMark checked={checked} />
            </Pressable>
          );
        })}

        <SectionLabel>Priority</SectionLabel>
        {PRIORITY_ORDER.map((priority) => {
          const checked = priorityFilters.includes(priority);
          return (
            <Pressable
              key={priority}
              onPress={() => onTogglePriority(priority)}
              className={cn(
                "flex-row items-center gap-3 px-4 py-2.5 active:bg-secondary",
                checked && "bg-secondary/60",
              )}
            >
              <PriorityIcon priority={priority} />
              <Text className="flex-1 text-sm text-foreground">
                {PRIORITY_LABEL[priority]}
              </Text>
              <CheckMark checked={checked} />
            </Pressable>
          );
        })}
      </ScrollView>
    </View>
  );
}

function useScopedFilters(
  scope: Scope,
  kind: "status",
): IssueStatus[];
function useScopedFilters(
  scope: Scope,
  kind: "priority",
): IssuePriority[];
function useScopedFilters(
  scope: Scope,
  kind: "status" | "priority",
): IssueStatus[] | IssuePriority[] {
  const allStatus = useIssuesViewStore((s) => s.statusFilters);
  const allPriority = useIssuesViewStore((s) => s.priorityFilters);
  const myStatus = useMyIssuesViewStore((s) => s.statusFilters);
  const myPriority = useMyIssuesViewStore((s) => s.priorityFilters);
  if (scope === "all") {
    return kind === "status" ? allStatus : allPriority;
  }
  return kind === "status" ? myStatus : myPriority;
}

function normalizeDateFilter(filter: IssueDateFilter): IssueDateFilter {
  return filter.from <= filter.to
    ? filter
    : { ...filter, from: filter.to, to: filter.from };
}

function SectionLabel({ children }: { children: string }) {
  return (
    <View className="px-4 pt-3 pb-1.5">
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {children}
      </Text>
    </View>
  );
}

function DateRangeCard({
  label,
  value,
  onChange,
  minimumDate,
  maximumDate,
}: {
  label: string;
  value: string;
  onChange: (date: Date | undefined) => void;
  minimumDate?: Date;
  maximumDate?: Date;
}) {
  return (
    <View className="gap-2 rounded-xl border border-border bg-card px-3 py-3">
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {label}
      </Text>
      <DateTimePicker
        value={dateOnlyToLocalDate(value) ?? new Date()}
        mode="date"
        display="inline"
        minimumDate={minimumDate}
        maximumDate={maximumDate}
        onChange={(_event, selected) => onChange(selected)}
      />
    </View>
  );
}

function CheckMark({ checked }: { checked: boolean }) {
  if (!checked) return null;
  return <Text className="text-sm text-primary font-semibold">✓</Text>;
}
