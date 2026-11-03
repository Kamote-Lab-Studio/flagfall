// Fonts are bundled with the theme so it works offline.
import "@fontsource/chakra-petch/500.css";
import "@fontsource/chakra-petch/600.css";
import "@fontsource/chakra-petch/700.css";
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/jetbrains-mono/800.css";
import "./app.css";

import { mount } from "svelte";
import App from "./App.svelte";

mount(App, { target: document.getElementById("app")! });
