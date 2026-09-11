import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Optional external audit dependency; application and normal tests do not need it.
export async function checkAccessibility(page, label) {
  const path = process.env.ACTA_AXE_SCRIPT;
  if (!path) return;
  if (!(await page.evaluate(() => !!window.axe)))
    await page.evaluate(readFileSync(path, "utf8"));
  const violations = await page.evaluate(async () => {
    const result = await window.axe.run(document, {
      runOnly: { type: "tag", values: ["wcag2a", "wcag2aa", "wcag21aa"] },
    });
    return result.violations
      .filter((v) => v.impact === "serious" || v.impact === "critical")
      .map((v) => ({
        id: v.id,
        nodes: v.nodes.map((n) => ({
          target: n.target,
          summary: n.failureSummary,
        })),
      }));
  });
  assert.deepEqual(
    violations,
    [],
    `${label}: serious/critical accessibility violations`,
  );
}
