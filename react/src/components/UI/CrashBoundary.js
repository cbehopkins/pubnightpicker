import React from "react";

class CrashBoundary extends React.Component {
    constructor(props) {
        super(props);
        this.state = { hasError: false, error: null };
    }

    static getDerivedStateFromError(error) {
        return { hasError: true, error };
    }

    componentDidCatch(error, info) {
        // Keep a console trace for deeper debugging while showing a readable in-app message.
        console.error("Unhandled render error:", error, info);
    }

    render() {
        if (this.state.hasError) {
            return (
                <main style={{ padding: "2rem", fontFamily: "sans-serif" }}>
                    <h1>Application Error</h1>
                    <p>The app crashed while rendering. Details:</p>
                    <pre style={{ whiteSpace: "pre-wrap" }}>
                        {this.state.error?.message || "Unknown error"}
                    </pre>
                </main>
            );
        }

        return this.props.children;
    }
}

export default CrashBoundary;
