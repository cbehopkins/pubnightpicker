import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import privacyNoticeMarkdown from "../../../docs/privacy-notice-draft.md?raw";

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

function PrivacyPage() {
    return (
        <div className="container py-4 py-md-5">
            <MarkdownNotice />
        </div>
    );
}

export default PrivacyPage;
