import { keepPreviousData, useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import {
  AlertTriangle,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  KeyRound,
  LayoutGrid,
  RefreshCw,
  RotateCcw,
  Search,
} from "lucide-react";
import { useDeferredValue, useMemo, useState } from "react";
import { toast } from "sonner";
import { KeyDialog } from "@/components/key-dialog";
import { LanguageToggle } from "@/components/language-toggle";
import { Logo } from "@/components/logo";
import { ResetCalendar } from "@/components/reset-calendar";
import { ThemeToggle } from "@/components/theme-toggle";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useManagementKey } from "@/hooks/use-management-key";
import i18n from "@/i18n";
import { useI18n } from "@/i18n/context";
import {
  fetchCredentials,
  fetchQuota,
  isQuotaCredential,
  keyFingerprint,
  resetQuota,
  UnauthorizedError,
} from "@/lib/api";
import { formatDateTime } from "@/lib/format";
import { bucketLabel, bucketTone, credentialName, detailText, providerLabel } from "@/lib/quota-ui";
import { cn } from "@/lib/utils";
import type { Credential, QuotaBucket, QuotaGroup, QuotaMetric, QuotaResponse } from "@/types";

const PAGE_SIZE = 12;
const RESET_PROVIDERS = new Set(["claude", "codex"]);

function metricText(metric: QuotaMetric, t: TFunction): string {
  if (metric.unit === "disabled") return t("summary.disabled");
  if (metric.unit === "unlimited") return t("summary.unlimited");
  if (metric.format === "currency" && metric.currency) {
    try {
      return new Intl.NumberFormat(undefined, { style: "currency", currency: metric.currency }).format(metric.value);
    } catch {
      return `${metric.value} ${metric.currency}`;
    }
  }
  if (metric.unit === "times") return `${metric.value} ${t("summary.times")}`;
  return metric.unit ? `${metric.value} ${metric.unit}` : String(metric.value);
}

function metricLabel(metric: QuotaMetric): string {
  const translated = i18n.t(`window.${metric.key}`, { defaultValue: "" });
  return translated || metric.label;
}

function BucketRow({ bucket, t }: { bucket: QuotaBucket; t: TFunction }) {
  const fraction = Math.max(0, Math.min(1, bucket.remainingFraction));
  const percent = Math.round(fraction * 100);
  const tone = bucketTone(fraction);
  const barTone = tone === "danger" ? "bg-destructive" : tone === "warning" ? "bg-warning" : "bg-success";
  const textTone = tone === "danger" ? "text-destructive" : tone === "warning" ? "text-warning" : "text-success";

  return (
    <div className="space-y-1">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1.5">
          <span className="max-w-28 shrink-0 truncate text-xs font-medium text-foreground">{bucketLabel(bucket)}</span>
          {bucket.description && (
            <Tooltip>
              <TooltipTrigger
                render={<span className="min-w-0 max-w-36 cursor-help truncate text-xs text-muted-foreground" />}
              >
                {detailText(bucket.description)}
              </TooltipTrigger>
              <TooltipContent className="max-w-xs warp-break-words">{detailText(bucket.description)}</TooltipContent>
            </Tooltip>
          )}
        </div>
        <span className={cn("shrink-0 font-mono text-xs font-semibold tabular-nums", textTone)}>{percent}%</span>
      </div>
      <div className="h-1 overflow-hidden rounded-full bg-muted">
        <div
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={percent}
          aria-label={bucketLabel(bucket)}
          className={cn("h-full rounded-full transition-all duration-300", barTone)}
          style={{ width: `${percent}%` }}
        />
      </div>
      {bucket.resetTime && (
        <p className="text-right text-[10px] text-muted-foreground">
          {t("quota.reset_at", { time: formatDateTime(bucket.resetTime) })}
        </p>
      )}
    </div>
  );
}

function GroupBlock({
  group,
  provider,
  showTitle,
  t,
}: {
  group: QuotaGroup;
  provider: string;
  showTitle: boolean;
  t: TFunction;
}) {
  const buckets = group.buckets ?? [];
  if (buckets.length === 0) return null;
  return (
    <div className="space-y-3">
      {showTitle && group.displayName && (
        <h4 className="text-xs font-medium text-muted-foreground">{group.displayName}</h4>
      )}
      <div className="space-y-3">
        {buckets.map((bucket) => (
          <BucketRow
            key={`${bucket.window ?? provider}-${bucket.resetTime ?? ""}-${bucket.description ?? ""}`}
            bucket={bucket}
            t={t}
          />
        ))}
      </div>
    </div>
  );
}

function QuotaCard({
  credential,
  quota,
  pending,
  error,
  fetching,
  onRefresh,
  onReset,
  t,
}: {
  credential: Credential;
  quota: QuotaResponse | undefined;
  pending: boolean;
  error: Error | null;
  fetching: boolean;
  onRefresh: () => void;
  onReset: () => void;
  t: TFunction;
}) {
  const provider = (credential.provider ?? credential.type ?? "").toLowerCase();
  const groups = quota?.groups ?? [];
  const summary = quota?.summary ?? [];
  const plan = quota?.subscription?.plan || quota?.subscription?.tierName;
  const canReset = RESET_PROVIDERS.has(provider);
  const name = credentialName(credential);

  return (
    <Card size="sm" className="gap-2">
      <CardHeader className="gap-1.5">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <h3 className="truncate text-sm font-medium" title={name}>
              {name}
            </h3>
            <div className="mt-1 flex flex-wrap items-center gap-1.5">
              <Badge variant="secondary" className="text-[10px] font-normal uppercase">
                {providerLabel(provider)}
              </Badge>
              {plan && (
                <Badge variant="outline" className="text-[10px] font-normal">
                  {plan}
                </Badge>
              )}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-0.5">
            {canReset && (
              <Button
                variant="ghost"
                size="icon-xs"
                title={t("quota.reset")}
                aria-label={t("quota.reset")}
                disabled={pending || Boolean(error)}
                onClick={onReset}
              >
                <RotateCcw className="size-3.5" />
              </Button>
            )}
            <Button
              variant="ghost"
              size="icon-xs"
              title={t("common.refresh")}
              aria-label={`${t("common.refresh")} ${name}`}
              disabled={fetching}
              onClick={onRefresh}
            >
              <RefreshCw className={cn("size-3.5", fetching && "animate-spin")} />
            </Button>
          </div>
        </div>
      </CardHeader>

      <CardContent className="space-y-3">
        {pending ? (
          <div className="space-y-2">
            <Skeleton className="h-6 w-full" />
            <Skeleton className="h-6 w-full" />
          </div>
        ) : error ? (
          <div className="rounded-md bg-destructive/10 p-2.5 text-xs text-destructive">
            <p className="font-medium">{t("quota.query_failed")}</p>
            <p className="mt-1 warp-break-words opacity-90">{error.message}</p>
            <Button variant="outline" size="sm" className="mt-2 h-6 text-xs" onClick={onRefresh}>
              {t("common.retry")}
            </Button>
          </div>
        ) : groups.length === 0 ? (
          <p className="py-2 text-center text-xs text-muted-foreground">{t("quota.no_details")}</p>
        ) : (
          <div className="space-y-2.5">
            {groups.map((group) => (
              <GroupBlock
                key={`${group.displayName ?? "group"}-${(group.buckets ?? []).map((bucket) => bucket.window ?? "").join("-")}`}
                group={group}
                provider={provider}
                showTitle={
                  groups.length > 1 || (group.displayName ?? "").toLowerCase() !== providerLabel(provider).toLowerCase()
                }
                t={t}
              />
            ))}
          </div>
        )}

        {summary.length > 0 && (
          <div className="flex flex-wrap gap-1.5 border-t pt-2">
            {summary.map((metric) => (
              <Badge
                key={metric.key}
                variant="outline"
                className="border-info/20 bg-info/10 text-[10px] font-normal text-info"
              >
                {metricLabel(metric)} · {metricText(metric, t)}
              </Badge>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function QuotaDashboard() {
  const { t } = useI18n();
  const { key, source, setKey, clear } = useManagementKey();
  const queryClient = useQueryClient();
  const [channel, setChannel] = useState("all");
  const [view, setView] = useState<"cards" | "calendar">("cards");
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const filterKey = `${channel}|${deferredSearch}`;
  const [pager, setPager] = useState({ filterKey, page: 1 });
  const page = pager.filterKey === filterKey ? pager.page : 1;
  const setPage = (next: number) => setPager({ filterKey, page: next });
  const [keyOpen, setKeyOpen] = useState(false);
  const [resetTarget, setResetTarget] = useState<Credential | null>(null);

  const credentials = useQuery({
    queryKey: ["quota-credentials", keyFingerprint(key)],
    queryFn: () => fetchCredentials(key),
    enabled: Boolean(key),
    retry: (count, error) => !(error instanceof UnauthorizedError) && count < 2,
  });

  const quotaCredentials = useMemo(() => (credentials.data ?? []).filter(isQuotaCredential), [credentials.data]);

  const results = useQueries({
    queries: quotaCredentials.map((credential) => ({
      queryKey: ["quota", keyFingerprint(key), credential.auth_index],
      queryFn: () => fetchQuota(key, credential.auth_index ?? ""),
      enabled: Boolean(key) && Boolean(credential.auth_index),
      retry: false,
      staleTime: 60_000,
      refetchOnWindowFocus: false,
      placeholderData: keepPreviousData,
    })),
  });

  const items = useMemo(
    () => quotaCredentials.map((credential, index) => ({ credential, query: results[index] })),
    [quotaCredentials, results],
  );

  const channels = useMemo(() => {
    const counts = new Map<string, number>();
    for (const item of items) {
      const provider = (item.credential.provider ?? item.credential.type ?? "").toLowerCase();
      counts.set(provider, (counts.get(provider) ?? 0) + 1);
    }
    return [...counts.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [items]);

  const filtered = useMemo(() => {
    const keyword = deferredSearch.trim().toLowerCase();
    return items
      .filter(({ credential }) => {
        const provider = (credential.provider ?? credential.type ?? "").toLowerCase();
        if (channel !== "all" && provider !== channel) return false;
        if (!keyword) return true;
        return [credentialName(credential), credential.provider, credential.label, credential.note]
          .filter(Boolean)
          .some((value) => String(value).toLowerCase().includes(keyword));
      })
      .sort((a, b) => credentialName(a.credential).localeCompare(credentialName(b.credential)));
  }, [channel, deferredSearch, items]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const current = Math.min(page, pageCount);
  const pageItems = filtered.slice((current - 1) * PAGE_SIZE, current * PAGE_SIZE);

  const refreshing = results.some((result) => result.isFetching);
  const unauthorized = credentials.error instanceof UnauthorizedError;
  const noKey = !key;

  const refreshAll = async () => {
    await Promise.all(results.map((result) => result.refetch()));
  };

  const reset = useMutation({
    mutationFn: (credential: Credential) => resetQuota(key, credential.auth_index ?? ""),
    onSuccess: (response, credential) => {
      if (response.success) {
        toast.success(t("quota.reset_done"));
      } else {
        toast.error(t("quota.reset_failed", { message: response.message ?? "" }));
      }
      queryClient.invalidateQueries({ queryKey: ["quota", keyFingerprint(key), credential.auth_index] });
      setResetTarget(null);
    },
    onError: (error: Error) => {
      toast.error(t("quota.reset_failed", { message: error.message }));
      setResetTarget(null);
    },
    meta: { quiet: true },
  });

  const showKeyPrompt = noKey || unauthorized;

  return (
    <div className="mx-auto max-w-7xl space-y-4 p-4 md:p-6">
      <header className="flex flex-col gap-3 border-b pb-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="flex min-w-0 items-center gap-2.5">
          <Logo className="size-8 shrink-0 rounded-md" />
          <div className="min-w-0">
            <h1 className="truncate text-lg font-semibold tracking-tight md:text-xl">{t("quota.title")}</h1>
            <p className="mt-0.5 text-xs text-muted-foreground">{t("quota.subtitle")}</p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="gap-1.5"
            onClick={() => void refreshAll()}
            disabled={!key || refreshing}
          >
            <RefreshCw className={cn(refreshing && "animate-spin")} />
            {refreshing ? t("quota.refreshing") : t("quota.refresh_all")}
          </Button>
          <Button variant="ghost" size="sm" className="gap-1.5" onClick={() => setKeyOpen(true)}>
            <KeyRound />
            {key ? t("key.connected") : t("key.missing")}
          </Button>
          <LanguageToggle />
          <ThemeToggle />
        </div>
      </header>

      {showKeyPrompt ? (
        <Card className="mx-auto flex max-w-lg flex-col items-center gap-3 p-8 text-center">
          <KeyRound className="size-8 text-muted-foreground" />
          <div className="space-y-1">
            <h2 className="text-sm font-medium">{noKey ? t("key.missing") : t("quota.query_failed")}</h2>
            <p className="text-xs text-muted-foreground">{t("key.desc")}</p>
          </div>
          <Button size="sm" onClick={() => setKeyOpen(true)}>
            {t("key.title")}
          </Button>
        </Card>
      ) : credentials.isPending ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {["a", "b", "c", "d", "e", "f"].map((id) => (
            <Card key={id} className="gap-3 p-4">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-8 w-full" />
              <Skeleton className="h-8 w-full" />
            </Card>
          ))}
        </div>
      ) : credentials.isError ? (
        <Card className="flex items-center gap-3 p-4 text-sm text-destructive">
          <AlertTriangle className="size-4" />
          {t("quota.load_failed", { message: credentials.error.message })}
        </Card>
      ) : quotaCredentials.length === 0 ? (
        <Card className="grid place-items-center gap-1 py-12 text-center">
          <p className="text-sm font-medium">{t("quota.empty")}</p>
          <p className="text-xs text-muted-foreground">{t("quota.empty_hint")}</p>
        </Card>
      ) : (
        <>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <Tabs value={channel} onValueChange={(value) => value && setChannel(value)}>
              <TabsList className="h-9 flex-wrap">
                <TabsTrigger value="all" className="gap-1.5 text-xs">
                  {t("common.all")}
                  <span className="rounded-full bg-muted-foreground/15 px-1.5 py-0.5 text-[10px] font-semibold">
                    {items.length}
                  </span>
                </TabsTrigger>
                {channels.map(([provider, count]) => (
                  <TabsTrigger key={provider} value={provider} className="gap-1.5 text-xs">
                    {providerLabel(provider)}
                    <span className="rounded-full bg-muted-foreground/15 px-1.5 py-0.5 text-[10px] font-semibold">
                      {count}
                    </span>
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>

            <div className="flex flex-wrap items-center gap-2">
              <div className="relative sm:w-56">
                <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t("quota.search_placeholder")}
                  className="h-8 pl-8 text-xs"
                />
              </div>
              <ToggleGroup
                value={[view]}
                onValueChange={(value) => {
                  const next = value[0];
                  if (next === "cards" || next === "calendar") setView(next);
                }}
                size="sm"
                aria-label={t("view.label")}
                className="bg-muted/60 p-0.5"
              >
                <ToggleGroupItem
                  value="cards"
                  aria-label={t("view.cards")}
                  title={t("view.cards")}
                  className="size-7 text-muted-foreground aria-pressed:bg-background! aria-pressed:text-foreground aria-pressed:shadow-sm"
                >
                  <LayoutGrid className="size-3.5" />
                </ToggleGroupItem>
                <ToggleGroupItem
                  value="calendar"
                  aria-label={t("view.calendar")}
                  title={t("view.calendar")}
                  className="size-7 text-muted-foreground aria-pressed:bg-background! aria-pressed:text-foreground aria-pressed:shadow-sm"
                >
                  <CalendarDays className="size-3.5" />
                </ToggleGroupItem>
              </ToggleGroup>
            </div>
          </div>

          {filtered.length === 0 ? (
            <Card className="grid place-items-center py-12 text-sm text-muted-foreground">{t("quota.no_match")}</Card>
          ) : view === "calendar" ? (
            <ResetCalendar items={filtered.map(({ credential, query }) => ({ credential, quota: query.data }))} />
          ) : (
            <>
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                {pageItems.map(({ credential, query }) => (
                  <QuotaCard
                    key={credential.auth_index ?? credential.id}
                    credential={credential}
                    quota={query.data}
                    pending={query.isPending}
                    error={query.error instanceof Error ? query.error : null}
                    fetching={query.isFetching}
                    onRefresh={() => void query.refetch()}
                    onReset={() => setResetTarget(credential)}
                    t={t}
                  />
                ))}
              </div>

              <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
                <span>
                  {current} / {pageCount}
                </span>
                <div className="flex items-center gap-1.5">
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1 text-xs"
                    disabled={current <= 1}
                    onClick={() => setPage(Math.max(1, current - 1))}
                  >
                    <ChevronLeft className="size-3.5" />
                    {t("common.previous")}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1 text-xs"
                    disabled={current >= pageCount}
                    onClick={() => setPage(Math.min(pageCount, current + 1))}
                  >
                    {t("common.next")}
                    <ChevronRight className="size-3.5" />
                  </Button>
                </div>
              </div>
            </>
          )}
        </>
      )}

      <KeyDialog
        open={keyOpen}
        onOpenChange={setKeyOpen}
        hasKey={Boolean(key)}
        source={source}
        error={unauthorized ? t("key.invalid") : ""}
        onSave={(value) => {
          setKey(value);
          setKeyOpen(false);
        }}
        onClear={clear}
      />

      <AlertDialog open={resetTarget !== null} onOpenChange={(open) => !open && setResetTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("quota.reset_confirm_title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("quota.reset_confirm_desc", {
                name: resetTarget ? credentialName(resetTarget) : "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={reset.isPending} onClick={() => resetTarget && reset.mutate(resetTarget)}>
              {reset.isPending && <RefreshCw className="animate-spin" />}
              {t("quota.reset")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
