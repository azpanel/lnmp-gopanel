import { useMemo, type ReactNode } from "react";
import type Prism from "prismjs";
import { tokenizeCrontab } from "@/lib/cron-highlight";

function renderTokens(
  value: string | Prism.Token | (string | Prism.Token)[],
): ReactNode {
  if (typeof value === "string") return value;
  if (Array.isArray(value))
    return value.map((token, index) => (
      <span key={index}>{renderTokens(token)}</span>
    ));
  const aliases = Array.isArray(value.alias)
    ? value.alias.join(" ")
    : value.alias || "";
  return (
    <span className={`token ${value.type} ${aliases}`}>
      {renderTokens(value.content)}
    </span>
  );
}

export function CronSource({ raw }: { raw: string }) {
  const tokens = useMemo(() => tokenizeCrontab(raw), [raw]);
  return (
    <pre aria-label="crontab 原文">
      <code className="language-crontab">
        {raw ? renderTokens(tokens) : "（空）"}
      </code>
    </pre>
  );
}
