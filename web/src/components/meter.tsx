export function Meter({ value }: { value: number | null | undefined }) {
  return (
    <div
      className="meter"
      role="progressbar"
      aria-label="使用率"
      aria-valuenow={value ?? undefined}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <span
        style={{
          width: `${Math.min(100, Math.max(0, value || 0))}%`,
          background: value && value > 90 ? "var(--danger)" : undefined,
        }}
      />
    </div>
  );
}
