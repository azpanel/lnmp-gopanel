import { useEffect, useSyncExternalStore } from "react";
import {
  activatePanelUpdates,
  getPanelUpdates,
  subscribePanelUpdates,
} from "./panel-updates";
import type { Session } from "./types";

export function usePanelUpdates(session: Session) {
  const updates = useSyncExternalStore(subscribePanelUpdates, getPanelUpdates);
  useEffect(() => {
    activatePanelUpdates(session);
  }, [session.username, session.expires_at]);
  return updates;
}
