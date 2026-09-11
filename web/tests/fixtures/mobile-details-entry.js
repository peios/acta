import { mount } from "svelte";
import "$lib/styles.css";
import "$lib/components/management/management.css";
import MobileDetails from "./MobileDetails.svelte";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture root");
mount(MobileDetails, { target });
