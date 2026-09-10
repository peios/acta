import { mount } from "svelte";
import Updates from "../../src/lib/components/settings/Updates.svelte";
import "../../src/lib/styles.css";
const target = document.getElementById("app");
if (!target) throw new Error("Missing fixture root");
mount(Updates, { target });
