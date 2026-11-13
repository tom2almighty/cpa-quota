import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { RefreshCw } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useI18n } from "@/i18n/context";
import { resetCooldown } from "@/lib/api";
import {
  ClaudeGrantError,
  type ClaudeResetGrant,
  claimRequestId,
  defaultGrantId,
  grantBlockReason,
  pendingClaim,
} from "@/lib/claude-grants";
import { claimClaudeGrant, readClaudeOrganization, readClaudeResetGrants } from "@/lib/claude-reset";
import { formatDateTime } from "@/lib/format";
import { credentialName } from "@/lib/quota-ui";
import { cn } from "@/lib/utils";
import type { Credential } from "@/types";

function grantKindLabel(t: TFunction, grant: ClaudeResetGrant): string {
  if (grant.kind === "weekly") return t("claude_reset.kind_weekly");
  if (grant.kind === "session") return t("claude_reset.kind_session");
  if (grant.kind === "full") return t("claude_reset.kind_full");
  return t("claude_reset.kind_other");
}

function blockReasonText(t: TFunction, reason: string): string {
  return t(`claude_reset.block_${reason}` as never);
}

function errorText(t: TFunction, error: Error): string {
  const code = error instanceof ClaudeGrantError ? error.code : "upstream";
  return t(`claude_reset.error_${code}` as never);
}

export function ClaudeResetDialog({
  open,
  credential,
  managementKey,
  keyId,
  onOpenChange,
  onCompleted,
}: {
  open: boolean;
  credential: Credential | null;
  managementKey: string;
  keyId: string;
  onOpenChange: (open: boolean) => void;
  onCompleted: (authIndex: string) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const groupName = useId();
  const authIndex = credential?.auth_index ?? "";
  const enabled = open && Boolean(managementKey) && Boolean(authIndex);
  const [selected, setSelected] = useState<string | null>(null);

  const statusQuery = useQuery({
    queryKey: ["claude-reset-grants", keyId, authIndex],
    queryFn: () => readClaudeResetGrants(managementKey, authIndex),
    enabled,
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
  });

  const status = statusQuery.data;
  const pending = enabled ? pendingClaim(authIndex) : null;
  const selectedGrant = status?.grants.find((grant) => grant.id === selected) ?? null;
  const selectedBlocked = selectedGrant && status ? grantBlockReason(status, selectedGrant) !== null : true;

  useEffect(() => {
    if (!status) return;
    if (pending) {
      setSelected(pending.grantId);
      return;
    }
    setSelected((current) => {
      const currentGrant = status.grants.find((grant) => grant.id === current);
      return currentGrant && grantBlockReason(status, currentGrant) === null ? current : defaultGrantId(status);
    });
  }, [status, pending?.grantId, pending]);

  const claim = useMutation({
    mutationFn: async (grantId: string) => {
      const organization = await readClaudeOrganization(managementKey, authIndex);
      const { requestId } = claimRequestId(authIndex, grantId);
      const result = await claimClaudeGrant(managementKey, { authIndex, organization, grantId, requestId });
      return { result, grantId };
    },
    onSuccess: async ({ result }) => {
      if (result === "reset" || result === "already_used") {
        let cooldownFailed = false;
        try {
          await resetCooldown(managementKey, authIndex);
        } catch {
          cooldownFailed = true;
        }
        if (cooldownFailed) toast.warning(t("claude_reset.cooldown_failed"));
        else toast.success(t(result === "reset" ? "claude_reset.success" : "claude_reset.already_used"));
        onCompleted(authIndex);
        onOpenChange(false);
        return;
      }
      toast.error(t(`claude_reset.result_${result}` as never));
      void statusQuery.refetch();
    },
    onError: (error: Error) => {
      toast.error(errorText(t, error));
      if (error instanceof ClaudeGrantError && error.code === "unknown_outcome") {
        void queryClient.invalidateQueries({ queryKey: ["claude-reset-grants", keyId, authIndex] });
      }
    },
  });

  const changeOpen = (next: boolean) => {
    if (!next && claim.isPending) return;
    onOpenChange(next);
  };

  const confirm = () => {
    if (!selected || selectedBlocked) return;
    claim.mutate(selected);
  };

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("claude_reset.title")}</DialogTitle>
          <DialogDescription>
            {t("claude_reset.desc", { name: credential ? credentialName(credential) : "" })}
          </DialogDescription>
        </DialogHeader>

        {pending && <p className="rounded-md bg-warning/10 p-2.5 text-xs text-warning">{t("claude_reset.pending")}</p>}

        {statusQuery.isPending ? (
          <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
            <RefreshCw className="size-4 animate-spin" />
            {t("claude_reset.loading")}
          </div>
        ) : statusQuery.isError ? (
          <div className="space-y-3 py-4">
            <p className="text-sm text-destructive">
              {t("claude_reset.load_failed", { message: errorText(t, statusQuery.error) })}
            </p>
            <Button variant="outline" size="sm" onClick={() => void statusQuery.refetch()}>
              <RefreshCw className={cn(statusQuery.isFetching && "animate-spin")} />
              {t("claude_reset.retry")}
            </Button>
          </div>
        ) : !status || status.grants.length === 0 ? (
          <p className="py-6 text-sm text-muted-foreground">
            {status && !status.eligible ? t("claude_reset.block_ineligible") : t("claude_reset.no_grants")}
          </p>
        ) : (
          <fieldset className="max-h-80 space-y-2 overflow-y-auto">
            <legend className="sr-only">{t("claude_reset.choose")}</legend>
            {status.grants.map((grant) => {
              const block = grantBlockReason(status, grant);
              const lockedByPending = pending !== null && pending.grantId !== grant.id;
              const disabled = block !== null || lockedByPending;
              const checked = selected === grant.id;
              return (
                <label
                  key={grant.id}
                  className={cn(
                    "flex cursor-pointer gap-2.5 rounded-md border p-2.5 text-xs transition-colors",
                    checked ? "border-info bg-info/5" : "border-border hover:bg-muted/50",
                    disabled && "cursor-not-allowed opacity-60 hover:bg-transparent",
                  )}
                >
                  <input
                    type="radio"
                    name={groupName}
                    value={grant.id}
                    checked={checked}
                    disabled={disabled}
                    onChange={() => setSelected(grant.id)}
                    className="mt-0.5 size-3.5 shrink-0 accent-info"
                  />
                  <span className="min-w-0 space-y-0.5">
                    <span className="block font-medium text-foreground">
                      {grantKindLabel(t, grant)} · {grant.label || grant.id}
                    </span>
                    <span className="block text-muted-foreground">
                      {t("claude_reset.left", { left: grant.resetsLeft, total: grant.resetsTotal })}
                    </span>
                    <span className="block text-muted-foreground">
                      {grant.endsAt
                        ? t("claude_reset.expires_at", { time: formatDateTime(grant.endsAt) })
                        : t("claude_reset.no_expiry")}
                    </span>
                    <span className="block text-muted-foreground">
                      {grant.givesBackWeekly ? t("claude_reset.hint_weekly") : t("claude_reset.hint_window")}
                    </span>
                    {block && <span className="block text-warning">{blockReasonText(t, block)}</span>}
                    {lockedByPending && (
                      <span className="block text-warning">{t("claude_reset.pending_other_grant")}</span>
                    )}
                  </span>
                </label>
              );
            })}
          </fieldset>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => changeOpen(false)} disabled={claim.isPending}>
            {t("common.cancel")}
          </Button>
          <Button onClick={confirm} disabled={!selected || selectedBlocked || claim.isPending || statusQuery.isPending}>
            {claim.isPending && <RefreshCw className="animate-spin" />}
            {claim.isPending ? t("claude_reset.confirming") : t("claude_reset.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
