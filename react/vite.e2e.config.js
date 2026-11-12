import { mergeConfig } from "vite";
import config from "./vite.config.js";

const baseConfig = config({ command: "serve", mode: "development" });

export default mergeConfig(baseConfig, {
    envDir: false,
    server: { open: false },
});
