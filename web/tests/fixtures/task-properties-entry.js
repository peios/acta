import { mount } from "svelte";
import TaskProperties from "./TaskProperties.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture root");
mount(TaskProperties, { target });
