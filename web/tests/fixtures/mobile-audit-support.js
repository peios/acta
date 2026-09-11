import { mobileViewport } from "$lib/mobile-viewport";
import "$lib/styles.css";
import "$lib/components/management/management.css";
// Optional broad mobile runs use production typography and viewport tracking.
if (new URL(location.href).searchParams.has("mobile-audit")) {
  const meta = document.createElement("meta");
  meta.name = "viewport";
  meta.content = "width=device-width, initial-scale=1";
  document.head.append(meta);
  mobileViewport(document.documentElement);
}
