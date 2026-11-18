import type { TrafficPeriod } from "./types";

export type TrafficView = {
  period: TrafficPeriod;
  chart: "line" | "bar";
};
const storageKey = "dashboard-website-traffic-view";
const defaultView: TrafficView = { period: "day", chart: "line" };

export function readTrafficView(): TrafficView {
  try {
    const value = JSON.parse(localStorage.getItem(storageKey) ?? "null");
    if (
      (value?.period === "day" || value?.period === "hour") &&
      (value?.chart === "line" || value?.chart === "bar")
    )
      return { period: value.period, chart: value.chart };
  } catch {
    // The widget also works when browser storage is unavailable.
  }
  return { ...defaultView };
}

export function saveTrafficView(view: TrafficView) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(view));
  } catch {
    // Keep the current page usable without persistent preferences.
  }
}
