import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config.js";

const baseConfig = viteConfig({ command: "serve", mode: "test" });

export default mergeConfig(baseConfig, defineConfig({
    test: {
        include: ["src/**/*.rules.test.js", "src/**/*.emulator.test.js"],
    },
}));
