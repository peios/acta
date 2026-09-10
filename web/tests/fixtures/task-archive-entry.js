import { mount } from "svelte";
import App from "./TaskArchiveReview.svelte";
const target = document.getElementById("app");
if (target) mount(App, { target });
