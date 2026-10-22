import React from "react";
import { Sun, Moon, Laptop } from "lucide-react";
import { useTheme } from "../context/ThemeContext";
import { Button } from "./ui/button";

export const ThemeToggle: React.FC = () => {
  const { theme, setTheme } = useTheme();

  const toggleTheme = () => {
    if (theme === "light") setTheme("dark");
    else if (theme === "dark") setTheme("system");
    else setTheme("light");
  };

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={toggleTheme}
      className="h-8 w-8 px-0 rounded-md border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition-colors"
      title={`Theme: ${theme} (Click to toggle)`}
      aria-label="Toggle theme"
    >
      {theme === "light" && <Sun className="h-4 w-4 text-amber-500" />}
      {theme === "dark" && <Moon className="h-4 w-4 text-indigo-400" />}
      {theme === "system" && <Laptop className="h-4 w-4 text-slate-500 dark:text-slate-400" />}
    </Button>
  );
};
