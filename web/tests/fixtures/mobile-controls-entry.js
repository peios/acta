import { mount } from "svelte";
import "$lib/styles.css";
import "$lib/components/management/management.css";
import MobileControls from "./MobileControls.svelte";
const target = document.getElementById("app");
if (!target) throw Error("Missing root");
mount(MobileControls, { target });
