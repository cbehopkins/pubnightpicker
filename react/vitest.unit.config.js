import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config.js";

export default mergeConfig(viteConfig, defineConfig({
    test: {
        maxWorkers: 4,
        include: ["src/**/*.test.js"],
        exclude: [
            "**/node_modules/**",
            "**/dist/**",
            "**/coverage/**",
            "src/**/*.rules.test.js",
        ],
    },
}));
