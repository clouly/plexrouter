import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./index.css";
import { App } from "./App";
import { I18nProvider, preloadInitialLang } from "./i18n";
import { ThemeProvider } from "./theme";
import { AuthProvider } from "./auth";
import { ToastProvider } from "./components/Toast";
import { ConfirmProvider } from "./components/Dialog";

function render() {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <BrowserRouter>
        <I18nProvider>
          <ThemeProvider>
            <ToastProvider>
              <ConfirmProvider>
                <AuthProvider>
                  <App />
                </AuthProvider>
              </ConfirmProvider>
            </ToastProvider>
          </ThemeProvider>
        </I18nProvider>
      </BrowserRouter>
    </StrictMode>,
  );
}

// Load the initial (non-English) dictionary before the first render so the UI never flashes
// English. preloadInitialLang never rejects; on failure the app starts in English.
void preloadInitialLang().then(render);
