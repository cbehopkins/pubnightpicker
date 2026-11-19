const loopbackHosts = new Set(["127.0.0.1", "localhost", "[::1]"]);

export async function advanceApplicationTime(instant, controlUrl = process.env.E2E_CLOCK_URL) {
    if (typeof instant !== "string" || !instant.trim()) {
        throw new Error("An explicit application instant is required");
    }
    if (!controlUrl) throw new Error("E2E_CLOCK_URL is required for application clock control");

    let endpoint;
    try {
        endpoint = new URL(controlUrl);
    } catch (error) {
        throw new Error("E2E_CLOCK_URL must be a loopback HTTP URL", { cause: error });
    }
    if (endpoint.protocol !== "http:" || !loopbackHosts.has(endpoint.hostname)) {
        throw new Error("E2E clock control must use a loopback HTTP URL");
    }

    const response = await fetch(new URL("/clock", endpoint), {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ instant }),
        signal: AbortSignal.timeout(5_000),
    });
    let acknowledgement;
    try {
        acknowledgement = await response.json();
    } catch (error) {
        throw new Error("E2E clock control returned an invalid response", { cause: error });
    }
    if (!response.ok) {
        throw new Error(acknowledgement.error || `E2E clock control failed with HTTP ${response.status}`);
    }
    return acknowledgement;
}
