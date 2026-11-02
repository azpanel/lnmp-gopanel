import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { captureSession } from "@/lib/session-scope";
import { policyMessage } from "@/lib/backup-policy-feedback";
import type {
  Destination,
  FileSource,
  PathsResult,
} from "@/lib/backup-policies";

export function useBackupPolicyPaths(
  files: FileSource[],
  destinations: Destination[],
) {
  const revision = JSON.stringify({ files, destinations });
  const hasContent =
    files.length > 0 ||
    destinations.some((d) =>
      d.type === "local"
        ? !!d.path
        : d.type === "gdrive"
          ? !!(d.account || d.folder_id)
          : !!(d.remote || d.path),
    );
  const [state, setState] = useState<{
    revision: string;
    result?: PathsResult;
    error?: string;
  }>();
  const scope = useRef(captureSession()).current;
  useEffect(() => {
    if (!hasContent) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      void api<PathsResult>("/backups/policies/validate-paths", {
        method: "POST",
        body: revision,
        signal: controller.signal,
      })
        .then((result) => {
          if (!controller.signal.aborted && scope.current())
            setState({ revision, result });
        })
        .catch((error: Error) => {
          if (!controller.signal.aborted && scope.current())
            setState({ revision, error: policyMessage(error.message) });
        });
    }, 450);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [revision, scope, hasContent]);
  const current = state?.revision === revision ? state : undefined;
  return {
    result: current?.result,
    error: current?.error,
    pending: hasContent && !current,
  };
}
