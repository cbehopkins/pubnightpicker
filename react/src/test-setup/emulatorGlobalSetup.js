import { spawn, execSync } from "child_process";
import { createConnection } from "net";

// Use a dedicated port for tests so it never clashes with an interactive emulator on 8080.
const TEST_FIRESTORE_PORT = 9080;

let emulatorProcess = null;

function waitForPort(port, host = "127.0.0.1", timeoutMs = 60000) {
    return new Promise((resolve, reject) => {
        const deadline = Date.now() + timeoutMs;
        function tryConnect() {
            const socket = createConnection({ port, host });
            socket.on("connect", () => {
                socket.destroy();
                resolve();
            });
            socket.on("error", () => {
                socket.destroy();
                if (Date.now() >= deadline) {
                    reject(
                        new Error(
                            `Timed out waiting for port ${port} after ${timeoutMs}ms`
                        )
                    );
                } else {
                    setTimeout(tryConnect, 500);
                }
            });
        }
        tryConnect();
    });
}

export async function setup() {
    // Expose the port to all test files via an environment variable.
    process.env.VITEST_FIRESTORE_PORT = String(TEST_FIRESTORE_PORT);

    // If the test emulator is already running (e.g. started by a previous run that
    // didn't tear down), skip startup to avoid a duplicate process.
    try {
        await waitForPort(TEST_FIRESTORE_PORT, "127.0.0.1", 1000);
        console.log(
            `\n[Global Setup] Firestore emulator already running on port ${TEST_FIRESTORE_PORT} — skipping start.`
        );
        return;
    } catch {
        // Not running yet — start it below.
    }

    console.log("\n[Global Setup] Starting Firestore emulator...");
    emulatorProcess = spawn(
        "npx",
        [
            "firebase-tools",
            "emulators:start",
            "--only",
            "firestore",
            "--config",
            "firebase.test.json",
            "--project",
            "pubnightpicker",
        ],
        {
            cwd: process.cwd(),
            stdio: "pipe",
            shell: true,
            // Detach so the child process group is independent; lets us kill the
            // tree cleanly and avoids keeping the Node event loop open.
            detached: true,
        }
    );

    emulatorProcess.stdout.on("data", (data) => {
        process.stdout.write(`[Emulator] ${data}`);
    });

    emulatorProcess.stderr.on("data", (data) => {
        process.stderr.write(`[Emulator] ${data}`);
    });

    await waitForPort(TEST_FIRESTORE_PORT, "127.0.0.1", 60000);
    console.log(`[Global Setup] Firestore emulator ready on port ${TEST_FIRESTORE_PORT}.\n`);
}

export async function teardown() {
    if (emulatorProcess) {
        console.log("\n[Global Setup] Stopping Firestore emulator...");
        const pid = emulatorProcess.pid;
        // Destroy the I/O streams first so they don't keep the event loop alive.
        emulatorProcess.stdout.destroy();
        emulatorProcess.stderr.destroy();
        emulatorProcess = null;
        try {
            // On Windows, SIGTERM doesn't kill the Java child process started by
            // firebase-tools. Use taskkill to force-kill the whole process tree.
            if (process.platform === "win32") {
                execSync(`taskkill /F /T /PID ${pid}`, { stdio: "ignore" });
            } else {
                process.kill(-pid, "SIGTERM");
            }
        } catch {
            // Process may have already exited — ignore.
        }
    }
}
