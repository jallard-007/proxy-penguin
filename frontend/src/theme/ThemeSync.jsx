import { useEffect } from "react";
import { useTheme } from "./ThemeContext";

export default function ThemeSync() {
  const { resolvedTheme } = useTheme();

  useEffect(() => {
      const isDark = resolvedTheme === "dark";
      document.documentElement.classList.toggle("dark", isDark);
      document.body.dataset.agThemeMode = resolvedTheme
  }, [resolvedTheme]);

  return null;
}
