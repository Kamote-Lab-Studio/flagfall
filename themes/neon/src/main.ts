// Fonts are bundled with the theme so it works offline.
import "@fontsource/oxanium/500.css";
import "@fontsource/oxanium/600.css";
import "@fontsource/oxanium/700.css";
import "@fontsource/barlow/400.css";
import "@fontsource/barlow/500.css";
import "@fontsource/barlow/600.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/800.css";
import "./app.css";

import { mount } from "svelte";
import App from "./App.svelte";

mount(App, { target: document.getElementById("app")! });
