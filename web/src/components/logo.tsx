/** 额度监控：蓝色圆角方块 + 白色仪表盘指针，与面板、用量插件的 logo 保持一致。 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" fill="none" aria-hidden className={className}>
      <rect width="32" height="32" rx="7" fill="#2a78d6" />
      <path d="M7.5 20a8.5 8.5 0 0 1 17 0" stroke="#fff" strokeWidth="2.6" strokeLinecap="round" />
      <path d="M16 20l4.5-4.5" stroke="#fff" strokeWidth="2.4" strokeLinecap="round" />
    </svg>
  );
}
