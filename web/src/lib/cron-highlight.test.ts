import { describe, expect, it } from "vitest";
import type Prism from "prismjs";
import { tokenizeCrontab } from "./cron-highlight";

function text(value: string | Prism.Token | (string | Prism.Token)[]): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map(text).join("");
  return text(value.content);
}

describe("crontab syntax highlighting", () => {
  it("preserves source, whitespace and shell characters across repeated renders", () => {
    const raw =
      '# backup\r\nSHELL=/bin/bash\n\t*/5\t* * * MON-FRI printf "<script>alert(1)</script>"  %data\n@reboot /root/start\n';
    for (let i = 0; i < 2; i++) {
      const tokens = tokenizeCrontab(raw);
      expect(text(tokens)).toBe(raw);
      expect(JSON.stringify(tokens)).toContain('"type":"schedule"');
      expect(JSON.stringify(tokens)).toContain('"type":"comment"');
      expect(JSON.stringify(tokens)).toContain('"type":"environment"');
      expect(JSON.stringify(tokens).match(/"type":"schedule"/g)).toHaveLength(
        2,
      );
    }
  });
  it("does not mistake command arguments for another schedule", () => {
    const tokens = tokenizeCrontab(
      "* * * * * echo one two three four five six\n",
    );
    expect(JSON.stringify(tokens).match(/"type":"schedule"/g)).toHaveLength(1);
    expect(text(tokens)).toBe("* * * * * echo one two three four five six\n");
  });
});
