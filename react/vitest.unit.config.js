import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config.js";

const baseConfig = viteConfig({ command: "serve", mode: "test" });

export default mergeConfig(baseConfig, defineConfig({
    envDir: false,
    test: {
        env: {
            VITE_FIREBASE_API_KEY: "",
        },
        maxWorkers: 4,
        include: ["src/**/*.test.js"],
        exclude: [
            "**/node_modules/**",
            "**/dist/**",
            "**/coverage/**",
            "src/**/*.rules.test.js",
            "src/**/*.emulator.test.js",
        ],
    },
}));
