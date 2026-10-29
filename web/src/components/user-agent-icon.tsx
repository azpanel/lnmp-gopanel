import { detectUserAgent } from "@/lib/user-agent";

export function UserAgentIcon({ userAgent }: { userAgent: string }) {
  const brand = detectUserAgent(userAgent);
  if (!brand) return null;
  return (
    <img
      key={brand.id}
      className="log-user-agent-icon"
      src={brand.icon}
      alt={brand.name}
      title={brand.name}
      width={16}
      height={16}
      data-brand={brand.id}
      data-monochrome={brand.monochrome || undefined}
      onError={(event) => {
        event.currentTarget.style.visibility = "hidden";
      }}
    />
  );
}
