import "./mobile-audit-support.js";
import { mount } from "svelte";
import TaskSearchReview from "./TaskSearchReview.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture root");
mount(TaskSearchReview, { target });
