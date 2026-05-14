import { useCallback, useState } from "react";
import ReactMarkdown from "react-markdown";
import { useSelector } from "react-redux";
import { Alert, Card } from "react-bootstrap";
import remarkGfm from "remark-gfm";
import privacyNoticeMarkdown from "../../../docs/privacy-notice.md?raw";
import Button from "../UI/Button";
import { downloadCurrentUserDataExport } from "../../utils/userDataExport";
import { notifyError } from "../../utils/notify";

function MarkdownNotice() {
    return (
        <div className="privacy-notice-markdown">
            <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                    h1: ({ ...props }) => <h1 className="h2 mb-2" {...props} />,
                    h2: ({ ...props }) => <h2 className="h5 mt-4 mb-3" {...props} />,
                    h3: ({ ...props }) => <h3 className="h6 mt-3 mb-2" {...props} />,
                    p: ({ ...props }) => <p className="mb-2" {...props} />,
                    ul: ({ ...props }) => <ul className="mb-3" {...props} />,
                    hr: () => <hr className="my-3" />,
                    a: ({ ...props }) => <a rel="noreferrer" {...props} />,
                    table: ({ ...props }) => (
                        <div className="table-responsive mb-3">
                            <table className="table table-striped table-bordered align-middle mb-0" {...props} />
                        </div>
                    ),
                }}
            >
                {privacyNoticeMarkdown}
            </ReactMarkdown>
        </div>
    );
}

function PrivacyExportPanel() {
    const uid = useSelector((state) => state.auth.uid);
    const loggedIn = useSelector((state) => state.auth.loggedIn);
    const [isExporting, setIsExporting] = useState(false);

    const handleExport = useCallback(async () => {
        if (!uid) {
            return;
        }

        setIsExporting(true);
        try {
            await downloadCurrentUserDataExport(uid);
        } catch (error) {
            console.error(error);
            notifyError(error?.message || "Unable to export your data.");
        } finally {
            setIsExporting(false);
        }
    }, [uid]);

    if (!loggedIn) {
        return (
            <Alert variant="secondary" className="mb-4">
                Sign in to export the data we hold about your account.
            </Alert>
        );
    }

    return (
        <Card className="mb-4">
            <Card.Body>
                <h2 className="h5 mb-2">Export Your Data</h2>
                <p className="mb-3 text-body-secondary">
                    Download a JSON copy of your account preferences, voting data, attendance records, and chat messages. The export includes both raw records and an expanded view with event details.
                </p>
                <Button type="button" onClick={handleExport} disabled={isExporting}>
                    {isExporting ? "Preparing export..." : "Download My Data (JSON)"}
                </Button>
            </Card.Body>
        </Card>
    );
}

function PrivacyPage() {
    return (
        <div className="container py-4 py-md-5">
            <PrivacyExportPanel />
            <MarkdownNotice />
        </div>
    );
}

export default PrivacyPage;
