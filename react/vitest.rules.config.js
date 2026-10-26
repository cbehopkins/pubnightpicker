import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config.js";

export default mergeConfig(viteConfig, defineConfig({
    test: {
        include: ["src/**/*.rules.test.js"],
    },
}));
