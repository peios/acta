import { mount } from "svelte";
import "$lib/styles.css";
import "$lib/components/management/management.css";
import App from "./ThreadHeaderReview.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture root");
mount(App, { target });
