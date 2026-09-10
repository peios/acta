import StarterKit from "@tiptap/starter-kit";
import Link from "@tiptap/extension-link";
import { Markdown } from "@tiptap/markdown";
import { TableKit } from "@tiptap/extension-table";
import TaskList from "@tiptap/extension-task-list";
import TaskItem from "@tiptap/extension-task-item";
// Mentions carry stable identity without pretending there is a public profile page.
const TaskLink = Link.extend({
  parseHTML() {
    return [
      ...(this.parent?.() || []),
      { tag: "span[data-account-reference]" },
    ];
  },
  renderHTML(props) {
    const href = props.HTMLAttributes.href;
    if (
      typeof href === "string" &&
      /^\/references\/accounts\/[a-f0-9-]{36}$/.test(href)
    )
      return [
        "span",
        {
          ...props.HTMLAttributes,
          "data-account-reference": "",
          class: "account-mention",
        },
        0,
      ];
    return this.parent?.(props) || ["a", props.HTMLAttributes, 0];
  },
});
export function taskExtensions() {
  return [
    StarterKit.configure({ link: false, underline: false }),
    TaskLink.configure({ openOnClick: false }),
    TableKit,
    TaskList,
    TaskItem.configure({ nested: true }),
    Markdown,
  ];
}
// Rich mode deliberately does not support attachments or raw HTML in this slice.
// Keep these documents in source mode rather than silently discarding content.
import { MarkdownManager } from "@tiptap/markdown";
import { getSchema } from "@tiptap/core";
import { DOMSerializer } from "@tiptap/pm/model";
const manager = new MarkdownManager({ extensions: taskExtensions() });
const schema = getSchema(taskExtensions());
const serializer = DOMSerializer.fromSchema(schema);
/** Render the same Markdown schema as the editor without mounting an editor.
 * @param {string} markdown
 * @returns {DocumentFragment}
 */
export function renderTaskMarkdown(markdown) {
  const fragment = document.createDocumentFragment();
  if (needsSourceMode(markdown)) {
    const source = document.createElement("pre");
    source.textContent = markdown;
    fragment.append(source);
    return fragment;
  }
  fragment.append(
    serializer.serializeFragment(
      schema.nodeFromJSON(manager.parse(markdown)).content,
    ),
  );
  fragment.querySelectorAll('input[type="checkbox"]').forEach((checkbox) => {
    checkbox.setAttribute("disabled", "");
    checkbox.setAttribute(
      "aria-label",
      checkbox.closest("li")?.textContent?.trim() || "Checklist item",
    );
  });
  return fragment;
}
/** @param {string} markdown @returns {boolean} */
export function needsSourceMode(markdown) {
  /** @param {unknown} value @returns {boolean} */
  const walk = (value) => {
    if (!value || typeof value !== "object") return false;
    if ("type" in value && (value.type === "image" || value.type === "html"))
      return true;
    return Object.values(value).some((v) =>
      Array.isArray(v) ? v.some(walk) : typeof v === "object" && walk(v),
    );
  };
  return manager.instance.lexer(markdown).some(walk);
}
