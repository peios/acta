import { createHash } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import adapter from "@sveltejs/adapter-static";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

const web = fileURLToPath(new URL(".", import.meta.url));

/**
 * The frontend's version: a hash of what it is built from, so that a browser
 * sees a new version exactly when the frontend changes, and two builds of one
 * source agree. SvelteKit's default, the build's time, does neither.
 */
function contentVersion() {
    const hash = createHash("sha256");
    const add = (path) => {
        hash.update(`${path}\0`);
        hash.update(readFileSync(join(web, path)));
    };
    const walk = (directory) => {
        const entries = readdirSync(join(web, directory), { withFileTypes: true });
        for (const entry of entries.sort((a, b) =>
            a.name < b.name ? -1 : a.name > b.name ? 1 : 0,
        )) {
            const path = join(directory, entry.name);
            if (entry.isDirectory()) {
                walk(path);
            } else {
                add(path);
            }
        }
    };
    walk("src");
    walk("static");
    for (const path of [
        "package.json",
        "package-lock.json",
        "svelte.config.js",
        "vite.config.ts",
        "../internal/tasks/properties.json",
    ]) {
        add(path);
    }
    return hash.digest("hex").slice(0, 16);
}

/** @type {import('@sveltejs/kit').Config} */
export default {
    preprocess: vitePreprocess(),
    kit: {
        adapter: adapter({ fallback: "200.html" }),
        version: { name: contentVersion() },
        csp: {
            mode: "hash",
            directives: {
                "default-src": ["self"],
                "script-src": ["self"],
                "style-src": ["self", "unsafe-inline"],
                "img-src": ["self", "data:"],
                "object-src": ["none"],
                "base-uri": ["none"],
                "form-action": ["self"],
            },
        },
    },
};
