import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { captureSession } from "@/lib/session-scope";
import type {
  Destination,
  FileSource,
  PathsResult,
} from "@/lib/backup-policies";

export function BackupPolicyPathCheck({
  files,
  destinations,
  onResult,
}: {
  files: FileSource[];
  destinations: Destination[];
  onResult: (result: PathsResult | undefined) => void;
}) {
  const revision = JSON.stringify({ files, destinations });
  const [state, setState] = useState<{
    revision: string;
    result?: PathsResult;
    error?: string;
  }>();
  const scope = useRef(captureSession()).current;
  const callback = useRef(onResult);
  callback.current = onResult;
  useEffect(() => {
    const controller = new AbortController();
    callback.current(undefined);
    const timer = setTimeout(() => {
      void api<PathsResult>("/backups/policies/validate-paths", {
        method: "POST",
        body: revision,
        signal: controller.signal,
      })
        .then((result) => {
          if (!controller.signal.aborted && scope.current()) {
            setState({ revision, result });
            callback.current(result);
          }
        })
        .catch((e: Error) => {
          if (!controller.signal.aborted && scope.current())
            setState({ revision, error: e.message });
        });
    }, 450);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [revision, scope]);
  const current = state?.revision === revision ? state : undefined;
  if (!files.length && !destinations.length) return null;
  return (
    <div className="policy-path-check" aria-live="polite">
      {!current && <p className="small-note">正在检查来源与存储位置…</p>}
      {current?.error && (
        <p className="small-note">
          路径预检失败：{current.error}。保存时将重新检查。
        </p>
      )}
      {current?.result?.errors.map((item, i) => (
        <p className="form-error" role="alert" key={`e${i}`}>
          {item.field === "destinations" ? "存储位置" : "来源"} {item.index + 1}
          ：{item.message}
        </p>
      ))}
      {current?.result?.warnings.map((item, i) => (
        <p className="small-note" key={`w${i}`}>
          {item.message}
        </p>
      ))}
    </div>
  );
}
