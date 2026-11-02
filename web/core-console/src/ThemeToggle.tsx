import { useEffect, useState } from "react";
import { Moon, Sun } from "lucide-react";

const key = "orbit.web.theme";
export default function ThemeToggle() {
  const [dark, setDark] = useState(
    () => document.documentElement.dataset.theme === "dark",
  );
  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const update = () => {
      let saved: string | null = null;
      try {
        saved = localStorage.getItem(key);
      } catch {
        /* optional preference */
      }
      const value = saved === "dark" || (saved !== "light" && media.matches);
      document.documentElement.dataset.theme = value ? "dark" : "light";
      setDark(value);
    };
    update();
    media.addEventListener("change", update);
    window.addEventListener("storage", update);
    return () => {
      media.removeEventListener("change", update);
      window.removeEventListener("storage", update);
    };
  }, []);
  return (
    <button
      className="icon-button"
      aria-label={dark ? "切换到浅色模式" : "切换到深色模式"}
      onClick={() => {
        const theme = dark ? "light" : "dark";
        document.documentElement.dataset.theme = theme;
        try {
          localStorage.setItem(key, theme);
        } catch {
          /* optional preference */
        }
        setDark(!dark);
      }}
    >
      {dark ? <Sun size={18} /> : <Moon size={18} />}
    </button>
  );
}
