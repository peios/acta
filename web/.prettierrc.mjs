import base from "../.prettierrc.json" with { type: "json" };

export default {
    ...base,
    plugins: ["prettier-plugin-svelte"],
    overrides: [{ files: "*.svelte", options: { parser: "svelte" } }],
};
