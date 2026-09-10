import { readable } from "svelte/store";

// One clock for every visible timestamp, regardless of feed or thread size.
export const relativeClock = readable(Date.now(), (set) => {
  if (typeof window === "undefined") return;
  const update = () => set(Date.now());
  update();
  const timer = setInterval(update, 30000);
  document.addEventListener("visibilitychange", update);
  return () => {
    clearInterval(timer);
    document.removeEventListener("visibilitychange", update);
  };
});

/** @param {string} date @param {number} now */
export function relativeTime(date, now) {
  const seconds = Math.max(0, Math.floor((now - Date.parse(date)) / 1000));
  if (!Number.isFinite(seconds)) return "Unknown time";
  if (seconds < 60) return "Just now";
  for (const [
    size,
    singular,
    plural,
  ] of /** @type {[number,string,string][]} */ ([
    [31536000, "year", "years"],
    [2592000, "month", "months"],
    [604800, "week", "weeks"],
    [86400, "day", "days"],
    [3600, "hour", "hours"],
    [60, "min", "mins"],
  ])) {
    if (seconds >= size) {
      const count = Math.floor(seconds / size);
      return `${count} ${count === 1 ? singular : plural} ago`;
    }
  }
  return "Just now";
}
