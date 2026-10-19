export type Credential = {
  id: string;
  auth_index?: string;
  name: string;
  provider?: string;
  type?: string;
  label?: string;
  note?: string;
  email?: string;
  account?: string;
  status?: string;
  disabled?: boolean;
  quota_plugin_id?: string;
};

export type CredentialsResponse = {
  files?: Credential[];
  total?: number;
  page?: number;
  page_size?: number;
  has_more?: boolean;
};

export type QuotaSubscription = {
  plan?: string;
  tierName?: string;
  tierId?: string;
};

export type QuotaBucket = {
  window?: string;
  remainingFraction: number;
  resetTime?: string;
  description?: string;
};

export type QuotaGroup = {
  displayName?: string;
  buckets?: QuotaBucket[];
};

export type QuotaMetric = {
  key: string;
  label: string;
  value: number;
  unit?: string;
  format?: string;
  currency?: string;
};

export type QuotaResponse = {
  subscription?: QuotaSubscription;
  summary?: QuotaMetric[];
  groups?: QuotaGroup[];
  serverTimeOffsetMs?: number;
};

export type QuotaResetResponse = {
  success: boolean;
  message?: string;
};

export type ProviderOption = {
  id: string;
  label: string;
  count: number;
};
