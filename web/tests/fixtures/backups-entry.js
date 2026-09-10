import { mount } from "svelte";
import Backups from "./Backups.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing backup fixture root");
mount(Backups, { target });
