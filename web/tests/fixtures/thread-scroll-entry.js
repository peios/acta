import { mount } from "svelte";
import ThreadScroll from "./ThreadScroll.svelte";
const target = document.getElementById("app");
if (!target) throw Error("Missing fixture root");
mount(ThreadScroll, { target });
