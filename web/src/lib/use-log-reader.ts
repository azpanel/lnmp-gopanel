import { useEffect, useRef, useState } from "react";
import { APIError } from "./api";
import { readLogWindow } from "./log-performance";
import { loadLogRules, mergeLogWindow, saveLogRules } from "./logs";
import type { LogRequest, LogRule, LogWindow } from "./logs";

type Command = { type: "tail" | "older" | "rules"; rules?: LogRule[] };
export function useLogReader(path: string) {
  const [pages, setPages] = useState<LogWindow[]>([]);
  const [busy, setBusy] = useState(false);
  const [paused, setPaused] = useState(false);
  const [error, setError] = useState("");
  const [changed, setChanged] = useState(false);
  const [evicted, setEvicted] = useState(false);
  const [ruleError, setRuleError] = useState("");
  const [follow, setFollow] = useState(true);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;
  const command = useRef<(c: Command) => Promise<boolean>>(async () => false);
  useEffect(() => {
    const controller = new AbortController();
    let alive = true,
      running = false,
      stopped = false,
      failures = 0;
    let cache: LogWindow[] = [],
      activeRules = loadLogRules(path);
    setPages([]);
    setBusy(false);
    setError("");
    setRuleError("");
    setChanged(false);
    setEvicted(false);
    setPaused(false);
    pausedRef.current = false;
    setFollow(true);
    const read = (
      request: Omit<LogRequest, "path" | "rules">,
      rules = activeRules,
    ) => {
      const body = JSON.stringify({ ...request, path, rules });
      if (new TextEncoder().encode(body).length > 4096)
        return Promise.reject(
          new APIError(
            400,
            "规则与游标组成的请求超过 4 KiB，请缩短规则内容或减少规则数量",
          ),
        );
      return readLogWindow(request.mode, {
        method: "POST",
        signal: controller.signal,
        body,
      });
    };
    const publish = (next: LogWindow[]) => {
      cache = next;
      if (alive) setPages(next);
    };
    const run = async (c?: Command): Promise<boolean> => {
      if (!path || !alive || running) return false;
      if (!c && (pausedRef.current || document.hidden || stopped)) return false;
      running = true;
      setBusy(true);
      try {
        if (c?.type === "rules") {
          const rules = c.rules!;
          let next: LogWindow[] = [];
          const ranges = cache.length ? cache : [null];
          for (const old of ranges) {
            let start = old?.start;
            do {
              const result = await read(
                old ? { mode: "range", start, end: old.end } : { mode: "tail" },
                rules,
              );
              if (!alive) return false;
              const merged = mergeLogWindow(next, result);
              next = merged.pages;
              if (merged.evicted) setEvicted(true);
              if (!old || BigInt(result.to) >= BigInt(old.to)) break;
              if (result.start === result.end)
                throw new Error("日志游标未前进，请回到最新日志");
              start = result.end;
            } while (alive);
          }
          if (!alive) return false;
          activeRules = rules;
          publish(next);
          setRuleError("");
          try {
            saveLogRules(path, rules);
          } catch {
            setRuleError("规则已应用，但浏览器无法保存设置");
          }
        } else {
          const first = cache[0],
            last = cache.at(-1);
          const tail = c?.type === "tail" || !last;
          const older = c?.type === "older" && !!first;
          const result = await read(
            tail
              ? { mode: "tail" }
              : older
                ? { mode: "before", start: first.start }
                : { mode: "after", end: last!.end },
          );
          if (!alive) return false;
          if (tail) {
            publish([result]);
            setEvicted(false);
          } else if (result.from !== result.to) {
            const merged = mergeLogWindow(cache, result, older);
            publish(merged.pages);
            if (merged.evicted) setEvicted(true);
          } else if (
            last &&
            (last.size !== result.size ||
              last.modified !== result.modified ||
              last.has_after !== result.has_after)
          ) {
            // Leave cached pages unchanged when an idle poll has no new data.
            // This also avoids rebuilding/sorting up to 20,000 virtual rows.
            publish(
              cache.map((p, i) =>
                i === cache.length - 1
                  ? {
                      ...p,
                      size: result.size,
                      modified: result.modified,
                      has_after: result.has_after,
                    }
                  : p,
              ),
            );
          }
        }
        if (!alive) return false;
        failures = 0;
        stopped = false;
        if (c?.type === "tail") setChanged(false);
        setError("");
        return true;
      } catch (e) {
        if (!alive) return false;
        if (c?.type === "rules" && e instanceof APIError && e.status === 400) {
          setRuleError(e.message);
        } else if (e instanceof APIError && e.status === 409) {
          setChanged(true);
          setError("日志已轮转或截断，正在重新读取最新日志。");
          publish([]);
          stopped = false;
        } else {
          setError((e as Error).message);
          failures++;
          if (e instanceof APIError && (e.status === 400 || e.status === 401))
            stopped = true;
        }
        return false;
      } finally {
        running = false;
        if (alive) setBusy(false);
      }
    };
    command.current = run;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      await run();
      if (alive)
        timer = setTimeout(
          () => void tick(),
          Math.min(30000, 1000 * 2 ** Math.min(failures, 5)),
        );
    };
    if (path) void tick();
    return () => {
      alive = false;
      controller.abort();
      clearTimeout(timer);
    };
  }, [path]);
  return {
    pages,
    busy,
    paused,
    setPaused,
    error,
    changed,
    evicted,
    ruleError,
    setRuleError,
    follow,
    setFollow,
    older: async () => {
      setPaused(true);
      pausedRef.current = true;
      setFollow(false);
      return command.current({ type: "older" });
    },
    latest: async () => {
      setPaused(false);
      pausedRef.current = false;
      setFollow(true);
      return command.current({ type: "tail" });
    },
    apply: (rules: LogRule[]) => command.current({ type: "rules", rules }),
  };
}
