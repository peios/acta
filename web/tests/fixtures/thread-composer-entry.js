import { mount } from "svelte";
import ThreadComposerReview from "./ThreadComposerReview.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture root");
mount(ThreadComposerReview, { target });
