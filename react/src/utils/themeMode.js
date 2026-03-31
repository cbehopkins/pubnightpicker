const THEME_STORAGE_KEY = "pnp-theme-mode";

const VALID_THEME_MODES = new Set(["light", "dark", "auto"]);

export const getStoredThemeMode = () => {
    if (typeof window === "undefined") {
        return "auto";
    }

    const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
    return VALID_THEME_MODES.has(stored) ? stored : "auto";
};

const getResolvedTheme = (mode) => {
    if (mode === "light" || mode === "dark") {
        return mode;
    }

    if (typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches) {
        return "dark";
    }

    return "light";
};

export const applyThemeMode = (mode) => {
    if (typeof document === "undefined") {
        return;
    }

    const resolved = getResolvedTheme(mode);
    const root = document.documentElement;
    root.setAttribute("data-bs-theme", resolved);
    root.style.colorScheme = resolved;
};

export const setStoredThemeMode = (mode) => {
    if (typeof window === "undefined" || !VALID_THEME_MODES.has(mode)) {
        return;
    }
    window.localStorage.setItem(THEME_STORAGE_KEY, mode);
};

export const subscribeToSystemThemeChanges = (onChange) => {
    if (typeof window === "undefined") {
        return () => { };
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    mediaQuery.addEventListener("change", onChange);

    return () => {
        mediaQuery.removeEventListener("change", onChange);
    };
};
