import assert from "node:assert/strict";
import { checkAccessibility } from "./mobile-accessibility.mjs";
const browsers = await import(
  process.env.ACTA_PLAYWRIGHT_MODULE || "playwright"
);
const engine = process.env.ACTA_MOBILE_BROWSER || "chromium";
const browser = await browsers[engine].launch({
  headless: true,
  executablePath: process.env.ACTA_MOBILE_EXECUTABLE || undefined,
});
try {
  // Uses the real locally served build, with a fresh unauthenticated browser.
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    ...(engine !== "firefox" ? { isMobile: true } : {}),
  });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("http://localhost:8081/login");
  const username = page.getByRole("textbox", { name: "Username", exact: true });
  await username.waitFor();
  for (const [width, height] of [
    [320, 568],
    [390, 844],
    [844, 390],
    [390, 380],
  ]) {
    await page.setViewportSize({ width, height });
    await username.fill("mobile-audit");
    await page
      .getByLabel("Password", { exact: true })
      .fill("not-a-real-password");
    await page
      .getByRole("button", { name: "Sign in", exact: true })
      .scrollIntoViewIfNeeded();
    assert.ok(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      "login does not overflow horizontally",
    );
    const submit = await page
      .getByRole("button", { name: "Sign in", exact: true })
      .boundingBox();
    assert.ok(
      submit.y >= 0 && submit.y + submit.height <= height + 1,
      "sign in is reachable at reduced height",
    );
    await checkAccessibility(page, `login ${width}x${height}`);
  }
  // Never send fake credentials to the server; exercise network failure locally.
  await page.route("**/api/login", (route) =>
    route.abort("internetdisconnected"),
  );
  await page.getByRole("button", { name: "Sign in", exact: true }).tap();
  await page.getByRole("alert").waitFor();
  assert.equal(await username.inputValue(), "mobile-audit");
  assert.equal(
    await page
      .getByRole("button", { name: "Sign in", exact: true })
      .isEnabled(),
    true,
  );
  assert.deepEqual(errors, []);
  console.log(
    `PASS ${engine}: served login, narrow/landscape/reduced-height layout, accessibility and failed-request recovery`,
  );
} finally {
  await browser.close();
}
