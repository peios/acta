import { mount } from "svelte";
import App from "./TaskBoardsReview.svelte";
const target = document.getElementById("app");
if (target) mount(App, { target });
