import { mergeConfig } from "vite";
import config from "./vite.config.js";

export default mergeConfig(config, {
    envDir: false,
    server: { open: false },
});
