import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import i18n from "@/i18n";
import { useI18n } from "@/i18n/context";
import { buildResetEvents, type QuotaViewItem, type ResetEvent } from "@/lib/quota-ui";
import { cn } from "@/lib/utils";

const DAYS = 7;

function startOfWeek(date: Date): Date {
  const next = new Date(date);
  next.setHours(0, 0, 0, 0);
  const offset = (next.getDay() + 6) % 7;
  next.setDate(next.getDate() - offset);
  return next;
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}

function dayKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

function toneClasses(tone: ResetEvent["tone"]): { chip: string; text: string } {
  switch (tone) {
    case "danger":
      return { chip: "border-destructive/30 bg-destructive/10", text: "text-destructive" };
    case "warning":
      return { chip: "border-warning/30 bg-warning/10", text: "text-warning" };
    default:
      return { chip: "border-success/30 bg-success/10", text: "text-success" };
  }
}

function EventChip({ event }: { event: ResetEvent }) {
  const { t } = useI18n();
  const locale = i18n.resolvedLanguage ?? "zh-CN";
  const tone = toneClasses(event.tone);
  const time = new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(event.at);
  const fullTime = new Intl.DateTimeFormat(locale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(event.at);

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            className={cn(
              "block w-full rounded-md border px-1.5 py-1 text-left transition-colors hover:bg-muted/50",
              tone.chip,
            )}
          />
        }
      >
        <span className="flex items-center justify-between gap-1 text-[10px]">
          <span className="font-mono text-muted-foreground tabular-nums">{time}</span>
          <span className={cn("font-semibold tabular-nums", tone.text)}>{event.percent}%</span>
        </span>
        <span className="mt-0.5 block truncate text-xs font-medium">{event.account}</span>
        <span className="block truncate text-[10px] text-muted-foreground">{event.label}</span>
      </TooltipTrigger>
      <TooltipContent side="top" className="max-w-xs">
        <div className="space-y-1">
          <p className="font-medium">{event.account}</p>
          <p className="text-muted-foreground">
            {event.provider.toUpperCase()} · {event.label}
          </p>
          {event.detail && <p className="text-muted-foreground">{event.detail}</p>}
          <p>
            {fullTime} · {t("calendar.event_remaining", { percent: event.percent })}
          </p>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

export function ResetCalendar({ items }: { items: QuotaViewItem[] }) {
  const { t } = useI18n();
  const locale = i18n.resolvedLanguage ?? "zh-CN";
  const [weekOffset, setWeekOffset] = useState(0);

  const events = useMemo(() => buildResetEvents(items), [items]);
  const weekStart = useMemo(() => addDays(startOfWeek(new Date()), weekOffset * DAYS), [weekOffset]);
  const days = useMemo(() => Array.from({ length: DAYS }, (_, index) => addDays(weekStart, index)), [weekStart]);
  const weekEnd = addDays(weekStart, DAYS);
  const weekEvents = useMemo(
    () => events.filter((event) => event.at >= weekStart && event.at < weekEnd),
    [events, weekStart, weekEnd],
  );
  const byDay = useMemo(() => {
    const map = new Map<string, ResetEvent[]>();
    for (const event of weekEvents) {
      const key = dayKey(event.at);
      const list = map.get(key) ?? [];
      list.push(event);
      map.set(key, list);
    }
    return map;
  }, [weekEvents]);

  const rangeLabel = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(weekStart);
  const rangeEnd = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(
    addDays(weekStart, DAYS - 1),
  );
  const todayKey = dayKey(new Date());

  return (
    <div className="space-y-3">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <CalendarDays className="size-4 text-info" />
          <div>
            <h3 className="text-sm font-medium">{t("calendar.title")}</h3>
            <p className="text-xs text-muted-foreground">{t("calendar.desc")}</p>
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <Button
            variant="outline"
            size="icon-xs"
            aria-label={t("calendar.prev")}
            title={t("calendar.prev")}
            onClick={() => setWeekOffset((value) => value - 1)}
          >
            <ChevronLeft className="size-3.5" />
          </Button>
          <span className="min-w-28 text-center text-xs font-medium tabular-nums">
            {rangeLabel} – {rangeEnd}
          </span>
          <Button
            variant="outline"
            size="icon-xs"
            aria-label={t("calendar.next")}
            title={t("calendar.next")}
            onClick={() => setWeekOffset((value) => value + 1)}
          >
            <ChevronRight className="size-3.5" />
          </Button>
          <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => setWeekOffset(0)}>
            {t("calendar.today")}
          </Button>
        </div>
      </div>

      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-7">
        {days.map((day) => {
          const key = dayKey(day);
          const dayEvents = byDay.get(key) ?? [];
          const isToday = key === todayKey;
          return (
            <div key={key} className="rounded-lg border bg-card p-2">
              <div className="mb-2 flex items-center justify-between gap-1">
                <span className="text-[10px] uppercase text-muted-foreground">
                  {new Intl.DateTimeFormat(locale, { weekday: "short" }).format(day)}
                </span>
                <span
                  className={cn(
                    "grid size-6 place-items-center rounded-full text-[11px] font-medium tabular-nums",
                    isToday ? "bg-primary text-primary-foreground" : "text-foreground",
                  )}
                >
                  {day.getDate()}
                </span>
              </div>
              <div className="space-y-1.5">
                {dayEvents.length === 0 ? (
                  <p className="py-2 text-center text-[10px] text-muted-foreground">{t("calendar.empty_day")}</p>
                ) : (
                  dayEvents.map((event) => <EventChip key={event.id} event={event} />)
                )}
              </div>
            </div>
          );
        })}
      </div>

      {weekEvents.length === 0 && (
        <p className="py-2 text-center text-xs text-muted-foreground">{t("calendar.no_events")}</p>
      )}
    </div>
  );
}
