import "./mobile-audit-support.js";
import { mount } from "svelte";
import TaskDocuments from "../../src/lib/components/tasks/TaskDocuments.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture root");
mount(TaskDocuments, {
  target,
  props: { task: "review", editable: true },
});
