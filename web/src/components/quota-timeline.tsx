import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import i18n from "@/i18n";
import { useI18n } from "@/i18n/context";
import {
  buildTimelineLane,
  projectTimelineWindows,
  providerLabel,
  type QuotaViewItem,
  type TimelineLane,
  type TimelineWindow,
} from "@/lib/quota-ui";
import { cn } from "@/lib/utils";

type Mode = "weekly" | "session";

const WEEK_DAYS = 14;
const SESSION_DAYS = 3;
const SESSION_CELLS = SESSION_DAYS * 4;

function startOfDay(date: Date): Date {
  const next = new Date(date);
  next.setHours(0, 0, 0, 0);
  return next;
}

function startOfWeek(date: Date): Date {
  const next = startOfDay(date);
  const offset = (next.getDay() + 6) % 7;
  next.setDate(next.getDate() - offset);
  return next;
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}

function spanFor(mode: Mode, offset: number, now: Date) {
  if (mode === "weekly") {
    const start = addDays(startOfWeek(now), offset * 7);
    return { start, end: addDays(start, WEEK_DAYS), days: WEEK_DAYS };
  }
  const start = addDays(startOfDay(now), offset);
  return { start, end: addDays(start, SESSION_DAYS), days: SESSION_DAYS };
}

function formatDate(date: Date, locale: string): string {
  return new Intl.DateTimeFormat(locale, { month: "2-digit", day: "2-digit" }).format(date);
}

function formatTime(date: Date, locale: string): string {
  return new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", hour12: false }).format(date);
}

function barTone(window: TimelineWindow, lane: TimelineLane): string {
  if (window.state === "past") return "border-border/60 bg-muted-foreground/10";
  if (window.state === "next") return "border-dashed border-border bg-muted/40";
  switch (lane.tone) {
    case "danger":
      return "border-destructive/40 bg-destructive/25 text-destructive";
    case "warning":
      return "border-warning/40 bg-warning/25 text-warning";
    default:
      return "border-success/40 bg-success/25 text-success";
  }
}

function TimelineBar({ lane, window, locale }: { lane: TimelineLane; window: TimelineWindow; locale: string }) {
  const { t } = useI18n();
  const start = new Date(window.startMs);
  const end = new Date(window.endMs);
  const remaining = window.remaining;
  const label = remaining === null ? t("timeline.unknown") : t("timeline.remaining", { percent: remaining });
  const range = `${formatDate(start, locale)} ${formatTime(start, locale)} – ${formatDate(end, locale)} ${formatTime(end, locale)}`;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            aria-label={`${lane.account} ${lane.label} ${label}`}
            className={cn(
              "absolute inset-y-1 flex items-center justify-center overflow-hidden rounded-sm border text-[10px] font-medium transition-colors hover:brightness-105",
              barTone(window, lane),
            )}
            style={{ left: `${window.leftPercent}%`, width: `${window.widthPercent}%` }}
          />
        }
      >
        {window.widthPercent >= 8 && remaining !== null ? `${remaining}%` : null}
      </TooltipTrigger>
      <TooltipContent side="top" className="max-w-xs">
        <div className="space-y-1">
          <p className="font-medium">{lane.account}</p>
          <p className="text-muted-foreground">
            {providerLabel(lane.provider)} · {lane.label}
          </p>
          {lane.detail && <p className="text-muted-foreground">{lane.detail}</p>}
          <p>
            {label} · {range}
          </p>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

function LaneRow({
  lane,
  spanStart,
  spanEnd,
  now,
  cells,
  locale,
}: {
  lane: TimelineLane;
  spanStart: number;
  spanEnd: number;
  now: number;
  cells: { key: string; isToday: boolean; isWeekend: boolean }[];
  locale: string;
}) {
  const windows = projectTimelineWindows(lane, spanStart, spanEnd, now);
  if (windows.length === 0) return null;
  const nowPercent = now >= spanStart && now < spanEnd ? ((now - spanStart) / (spanEnd - spanStart)) * 100 : null;

  return (
    <div className="flex border-b last:border-b-0">
      <div className="w-48 shrink-0 border-r px-3 py-2">
        <p className="truncate text-xs font-medium" title={lane.account}>
          {lane.account}
        </p>
        <div className="mt-1 flex items-center gap-1.5">
          <Badge variant="secondary" className="shrink-0 text-[10px] font-normal uppercase">
            {providerLabel(lane.provider)}
          </Badge>
          <span className="truncate text-[10px] text-muted-foreground">{lane.label}</span>
          <span
            className={cn(
              "ml-auto shrink-0 font-mono text-[10px] font-semibold tabular-nums",
              lane.tone === "danger" ? "text-destructive" : lane.tone === "warning" ? "text-warning" : "text-success",
            )}
          >
            {lane.remaining}%
          </span>
        </div>
      </div>
      <div className="relative min-w-0 flex-1 py-2">
        <div className="absolute inset-0 flex" aria-hidden>
          {cells.map((cell) => (
            <div
              key={cell.key}
              className={cn(
                "flex-1 border-r border-border/40 last:border-r-0",
                cell.isToday && "bg-primary/5",
                cell.isWeekend && !cell.isToday && "bg-muted/20",
              )}
            />
          ))}
        </div>
        <div className="relative h-7">
          {windows.map((window) => (
            <TimelineBar key={window.id} lane={lane} window={window} locale={locale} />
          ))}
          {nowPercent !== null && (
            <div
              className="pointer-events-none absolute inset-y-0 w-px bg-foreground/40"
              style={{ left: `${nowPercent}%` }}
              aria-hidden
            />
          )}
        </div>
      </div>
    </div>
  );
}

export function QuotaTimeline({ items, loading = false }: { items: QuotaViewItem[]; loading?: boolean }) {
  const { t } = useI18n();
  const locale = i18n.resolvedLanguage ?? "zh-CN";
  const [mode, setMode] = useState<Mode>("weekly");
  const [offset, setOffset] = useState(0);

  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, []);
  const span = useMemo(() => spanFor(mode, offset, new Date(now)), [mode, offset, now]);
  const spanStart = span.start.getTime();
  const spanEnd = span.end.getTime();
  const spanHours = (spanEnd - spanStart) / 3_600_000;

  const lanes = useMemo(
    () =>
      items
        .map((item) => buildTimelineLane(item, mode === "session" ? 6 : spanHours))
        .filter((lane): lane is TimelineLane => {
          if (!lane) return false;
          // 周视图不画 5 小时这类短窗口，避免一条泳道塞进几十个碎片；
          // 短窗口统一放到 5 小时视图里看。
          return mode === "session" ? lane.periodHours <= 6 : lane.periodHours >= 12;
        })
        .filter((lane) => projectTimelineWindows(lane, spanStart, spanEnd, now).length > 0),
    [items, mode, now, spanEnd, spanHours, spanStart],
  );

  const cellCount = mode === "session" ? SESSION_CELLS : WEEK_DAYS;
  const cells = useMemo(() => {
    const cellMs = (spanEnd - spanStart) / cellCount;
    const todayStart = startOfDay(new Date(now)).getTime();
    return Array.from({ length: cellCount }, (_, index) => {
      const at = new Date(spanStart + index * cellMs);
      const isDayStart = mode === "weekly" || at.getHours() === 0;
      return {
        key: `${at.getTime()}`,
        at,
        isDayStart,
        isToday: startOfDay(at).getTime() === todayStart,
        isWeekend: at.getDay() === 0 || at.getDay() === 6,
        label: isDayStart ? formatDate(at, locale) : formatTime(at, locale),
        weekday: new Intl.DateTimeFormat(locale, { weekday: "short" }).format(at),
      };
    });
  }, [cellCount, locale, mode, now, spanEnd, spanStart]);

  const rangeLabel = `${formatDate(span.start, locale)} – ${formatDate(addDays(span.end, -1), locale)}`;
  const empty = lanes.length === 0;

  return (
    <div className="space-y-3">
      <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h3 className="text-sm font-medium">{t("timeline.title")}</h3>
          <p className="text-xs text-muted-foreground">{t("timeline.desc")}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <ToggleGroup
            value={[mode]}
            onValueChange={(value) => {
              const next = value[0];
              if (next !== "weekly" && next !== "session") return;
              setMode(next);
              setOffset(0);
            }}
            size="sm"
            aria-label={t("timeline.mode_label")}
            className="bg-muted/60 p-0.5"
          >
            <ToggleGroupItem
              value="weekly"
              className="h-7 px-2 text-xs text-muted-foreground aria-pressed:bg-background! aria-pressed:text-foreground aria-pressed:shadow-sm"
            >
              {t("timeline.mode_weekly")}
            </ToggleGroupItem>
            <ToggleGroupItem
              value="session"
              className="h-7 px-2 text-xs text-muted-foreground aria-pressed:bg-background! aria-pressed:text-foreground aria-pressed:shadow-sm"
            >
              {t("timeline.mode_session")}
            </ToggleGroupItem>
          </ToggleGroup>
          <div className="flex items-center gap-1.5">
            <Button
              variant="outline"
              size="icon-xs"
              aria-label={t("timeline.prev")}
              title={t("timeline.prev")}
              onClick={() => setOffset((value) => value - 1)}
            >
              <ChevronLeft className="size-3.5" />
            </Button>
            <span className="min-w-28 text-center text-xs font-medium tabular-nums">{rangeLabel}</span>
            <Button
              variant="outline"
              size="icon-xs"
              aria-label={t("timeline.next")}
              title={t("timeline.next")}
              onClick={() => setOffset((value) => value + 1)}
            >
              <ChevronRight className="size-3.5" />
            </Button>
            <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => setOffset(0)}>
              {t("timeline.today")}
            </Button>
          </div>
        </div>
      </div>

      {loading && empty ? (
        <div className="space-y-2 rounded-lg border bg-card p-3">
          {["a", "b", "c", "d", "e"].map((id) => (
            <Skeleton key={id} className="h-10 w-full" />
          ))}
        </div>
      ) : empty ? (
        <div className="rounded-lg border bg-card py-8 text-center text-xs text-muted-foreground">
          {mode === "session" ? t("timeline.empty_session") : t("timeline.empty")}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border bg-card">
          <div className="min-w-160">
            <div className="flex border-b bg-muted/20">
              <div className="w-48 shrink-0 border-r px-3 py-2 text-xs font-medium text-muted-foreground">
                {t("timeline.credential")}
              </div>
              <div className="flex min-w-0 flex-1">
                {cells.map((cell) => (
                  <div
                    key={cell.key}
                    className={cn(
                      "flex-1 border-r border-border/40 px-1 py-1.5 text-center last:border-r-0",
                      cell.isToday && "bg-primary/5",
                      cell.isWeekend && !cell.isToday && "bg-muted/20",
                    )}
                  >
                    <div className="text-[10px] text-muted-foreground">{cell.isDayStart ? cell.weekday : ""}</div>
                    <div className={cn("text-[10px] tabular-nums", cell.isToday && "font-semibold text-primary")}>
                      {cell.label}
                    </div>
                  </div>
                ))}
              </div>
            </div>
            <div>
              {lanes.map((lane) => (
                <LaneRow
                  key={lane.credential.auth_index ?? lane.credential.id}
                  lane={lane}
                  spanStart={spanStart}
                  spanEnd={spanEnd}
                  now={now}
                  cells={cells}
                  locale={locale}
                />
              ))}
            </div>
          </div>
        </div>
      )}
      {!empty && <p className="text-[10px] text-muted-foreground">{t("timeline.hint")}</p>}
    </div>
  );
}
