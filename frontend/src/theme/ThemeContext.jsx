import { createContext, useContext, useEffect, useMemo, useState } from "react";

const ThemeContext = createContext(null);

function getSystemTheme() {
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

function getInitialPreference() {
  const saved = localStorage.getItem("themePreference");

  if (saved === "light" || saved === "dark" || saved === "system") {
    return saved;
  }

  return "system";
}

export function ThemeProvider({ children }) {
  const [themePreference, setThemePreference] = useState(getInitialPreference);
  const [systemTheme, setSystemTheme] = useState(getSystemTheme);

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");

    function handleChange(event) {
      setSystemTheme(event.matches ? "dark" : "light");
    }

    setSystemTheme(mediaQuery.matches ? "dark" : "light");
    mediaQuery.addEventListener("change", handleChange);

    return () => {
      mediaQuery.removeEventListener("change", handleChange);
    };
  }, []);

  useEffect(() => {
    localStorage.setItem("themePreference", themePreference);
  }, [themePreference]);

  const resolvedTheme =
    themePreference === "system" ? systemTheme : themePreference;

  const value = useMemo(() => {
    return {
      themePreference,
      resolvedTheme,
      systemTheme,
      setThemePreference,
      setThemeLight: () => setThemePreference("light"),
      setThemeDark: () => setThemePreference("dark"),
      setThemeSystem: () => setThemePreference("system"),
      toggleTheme: () => {
        setThemePreference((current) => {
          const effective = current === "system" ? systemTheme : current;
          return effective === "light" ? "dark" : "light";
        });
      },
    };
  }, [themePreference, resolvedTheme, systemTheme]);

  return (
    <ThemeContext.Provider value={value}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const context = useContext(ThemeContext);

  if (!context) {
    throw new Error("useTheme must be used inside ThemeProvider");
  }

  return context;
}
