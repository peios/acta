import { mount } from "svelte";
import Review from "./NotificationReview.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture target");
mount(Review, { target });
