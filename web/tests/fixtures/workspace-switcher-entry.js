import { mount } from "svelte";
import "$lib/styles.css";
import WorkspaceSwitcherReview from "./WorkspaceSwitcherReview.svelte";
const target = document.getElementById("app");
if (!target) throw Error("Missing root");
mount(WorkspaceSwitcherReview, { target });
