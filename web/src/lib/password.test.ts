import { describe, expect, it } from "vitest";
import { assessPassword } from "./password";
import cases from "./password-cases.json";

describe("login password acceptance shared with the server", () => {
  it.each(cases.map((item, index) => ({ ...item, index })))(
    "policy case $index",
    async ({ password, valid }) => {
      expect((await assessPassword(password, "admin")).valid).toBe(valid);
    },
  );
});
