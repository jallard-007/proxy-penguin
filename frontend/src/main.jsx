import "./index.css";
import "./GridModules";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ThemeProvider } from "./theme/ThemeContext"
import App from "./App"

// Render GridExample
const root = createRoot(document.getElementById("app"));
root.render(
    <StrictMode>
        <ThemeProvider>
            <App />
        </ThemeProvider>
    </StrictMode>,
);
