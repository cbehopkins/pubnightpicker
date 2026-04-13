import { useCallback, useState } from "react";
import ReactMarkdown from "react-markdown";
import { useSelector } from "react-redux";
import { Alert, Card } from "react-bootstrap";
import { collection, doc, getDoc, getDocs, query, where } from "firebase/firestore";
import remarkGfm from "remark-gfm";
import privacyNoticeMarkdown from "../../../docs/privacy-notice.md?raw";
import Button from "../UI/Button";
import { db } from "../../firebase";
import getUserDoc from "../../dbtools/getUserDoc";
import { notifyError } from "../../utils/notify";

/**
 * @param {string} userId
 * @param {Record<string, string[]>} votesData
 */
function extractVotesForUser(userId, votesData) {
    return Object.entries(votesData || {}).reduce((acc, [venueId, voterIds]) => {
        if (Array.isArray(voterIds) && voterIds.includes(userId)) {
            acc[venueId] = true;
        }
        return acc;
    }, {});
}

/**
 * @param {string} userId
 * @param {Record<string, { canCome?: string[], cannotCome?: string[] } | undefined>} attendanceData
 */
function extractAttendanceForUser(userId, attendanceData) {
    return Object.entries(attendanceData || {}).reduce((acc, [venueId, entry]) => {
        if (!entry) {
            return acc;
        }

        const canCome = Array.isArray(entry.canCome) && entry.canCome.includes(userId);
        const cannotCome = Array.isArray(entry.cannotCome) && entry.cannotCome.includes(userId);

        if (canCome || cannotCome) {
            acc[venueId] = {
                canCome,
                cannotCome,
            };
        }
        return acc;
    }, {});
}

function downloadJson(filename, payload) {
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const objectUrl = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = objectUrl;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(objectUrl);
}

function toTimestampIso(value) {
    if (!value) {
        return null;
    }
    if (typeof value?.toDate === "function") {
        return value.toDate().toISOString();
    }
    if (typeof value === "string") {
        return value;
    }
    return null;
}

function buildVenueDetails(venueId, pollVenueData, pubsById) {
    if (!venueId) {
        return null;
    }

    if (venueId === "any") {
        return {
            venueId,
            name: "Any venue",
            pollEntry: null,
            venueRecord: null,
        };
    }

    return {
        venueId,
        name: pollVenueData?.name || pubsById[venueId]?.name || null,
        pollEntry: pollVenueData || null,
        venueRecord: pubsById[venueId] || null,
    };
}

function buildPollDetails(pollId, pollsById, pubsById) {
    const pollData = pollsById[pollId] || null;
    const pollPubs = pollData?.pubs || {};
    const venueEntries = Object.entries(pollPubs)
        .filter(([venueId]) => venueId !== "any")
        .map(([venueId, entry]) => buildVenueDetails(venueId, entry, pubsById));

    return {
        pollId,
        date: pollData?.date || null,
        completed: Boolean(pollData?.completed),
        selectedVenue: buildVenueDetails(pollData?.selected || null, pollPubs[pollData?.selected || ""], pubsById),
        restaurantVenue: buildVenueDetails(pollData?.restaurant || null, pollPubs[pollData?.restaurant || ""], pubsById),
        restaurantTime: pollData?.restaurant_time || null,
        venuesOnPoll: venueEntries,
        rawPoll: pollData,
    };
}

function buildExpandedVotes(votesByPoll, pollsById, pubsById) {
    return Object.entries(votesByPoll).reduce((acc, [pollId, pollVotes]) => {
        acc[pollId] = {
            event: buildPollDetails(pollId, pollsById, pubsById),
            votes: Object.keys(pollVotes).map((venueId) => ({
                venue: buildVenueDetails(
                    venueId,
                    pollsById[pollId]?.pubs?.[venueId],
                    pubsById
                ),
                selected: true,
            })),
        };
        return acc;
    }, {});
}

function buildExpandedAttendance(attendanceByPoll, pollsById, pubsById) {
    return Object.entries(attendanceByPoll).reduce((acc, [pollId, pollAttendance]) => {
        acc[pollId] = {
            event: buildPollDetails(pollId, pollsById, pubsById),
            attendance: Object.entries(pollAttendance).map(([venueId, status]) => ({
                venue: buildVenueDetails(
                    venueId,
                    pollsById[pollId]?.pubs?.[venueId],
                    pubsById
                ),
                status,
            })),
        };
        return acc;
    }, {});
}

function buildLookupSection(votesByPoll, attendanceByPoll, pollsById, pubsById) {
    const pollIds = [...new Set([
        ...Object.keys(votesByPoll || {}),
        ...Object.keys(attendanceByPoll || {}),
    ])];

    const venueIds = new Set();
    pollIds.forEach((pollId) => {
        const voteVenueIds = Object.keys(votesByPoll?.[pollId] || {});
        const attendanceVenueIds = Object.keys(attendanceByPoll?.[pollId] || {});
        const pollVenueIds = Object.keys(pollsById?.[pollId]?.pubs || {});

        [...voteVenueIds, ...attendanceVenueIds, ...pollVenueIds].forEach((venueId) => {
            if (venueId) {
                venueIds.add(venueId);
            }
        });
    });

    return {
        polls: pollIds.reduce((acc, pollId) => {
            acc[pollId] = buildPollDetails(pollId, pollsById, pubsById);
            return acc;
        }, {}),
        venues: [...venueIds].reduce((acc, venueId) => {
            if (venueId === "any") {
                acc[venueId] = {
                    venueId,
                    name: "Any venue",
                    venueRecord: null,
                };
                return acc;
            }

            acc[venueId] = {
                venueId,
                name: pubsById?.[venueId]?.name || null,
                venueRecord: pubsById?.[venueId] || null,
            };
            return acc;
        }, {}),
    };
}

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
            const [privateUserDoc, publicUserSnapshot, votesSnapshot, attendanceSnapshot, pollsSnapshot, pubsSnapshot, messagesSnapshot] = await Promise.all([
                getUserDoc(uid),
                getDoc(doc(db, "user-public", uid)),
                getDocs(collection(db, "votes")),
                getDocs(collection(db, "attendance")),
                getDocs(collection(db, "polls")),
                getDocs(collection(db, "pubs")),
                getDocs(query(collection(db, "messages"), where("uid", "==", uid))),
            ]);

            const privateUserData = privateUserDoc ? privateUserDoc.data() : null;
            const publicUserData = publicUserSnapshot.exists() ? publicUserSnapshot.data() : null;
            const pollsById = pollsSnapshot.docs.reduce((acc, pollDoc) => {
                acc[pollDoc.id] = pollDoc.data();
                return acc;
            }, {});
            const pubsById = pubsSnapshot.docs.reduce((acc, pubDoc) => {
                acc[pubDoc.id] = pubDoc.data();
                return acc;
            }, {});

            const votesByPoll = votesSnapshot.docs.reduce((acc, voteDoc) => {
                const userVotes = extractVotesForUser(uid, voteDoc.data());
                if (Object.keys(userVotes).length > 0) {
                    acc[voteDoc.id] = userVotes;
                }
                return acc;
            }, {});

            const attendanceByPoll = attendanceSnapshot.docs.reduce((acc, attendanceDoc) => {
                const userAttendance = extractAttendanceForUser(uid, attendanceDoc.data());
                if (Object.keys(userAttendance).length > 0) {
                    acc[attendanceDoc.id] = userAttendance;
                }
                return acc;
            }, {});

            const rawMessages = messagesSnapshot.docs.map((messageDoc) => ({
                id: messageDoc.id,
                ...messageDoc.data(),
            }));

            const expandedMessages = rawMessages.map((message) => ({
                id: message.id,
                text: message.text || "",
                sentAt: toTimestampIso(message.createdAt),
                nameAtSendTime: message.name || null,
                uid: message.uid || null,
            }));

            const exportPayload = {
                exportedAt: new Date().toISOString(),
                userId: uid,
                lookup: buildLookupSection(votesByPoll, attendanceByPoll, pollsById, pubsById),
                raw: {
                    profile: {
                        private: privateUserData,
                        public: publicUserData,
                    },
                    votes: votesByPoll,
                    attendance: attendanceByPoll,
                    messages: rawMessages,
                },
                expanded: {
                    profile: {
                        preferredName: publicUserData?.name || privateUserData?.name || null,
                        email: privateUserData?.email || null,
                        notificationPreferences: {
                            notificationEmail: privateUserData?.notificationEmail || null,
                            notificationEmailEnabled: privateUserData?.notificationEmailEnabled || false,
                            openPollEmailEnabled: privateUserData?.openPollEmailEnabled || false,
                            votesVisible: privateUserData?.votesVisible || false,
                        },
                        publicProfile: publicUserData,
                    },
                    votes: buildExpandedVotes(votesByPoll, pollsById, pubsById),
                    attendance: buildExpandedAttendance(attendanceByPoll, pollsById, pubsById),
                    messages: expandedMessages,
                },
            };

            downloadJson(`pub-night-picker-data-${uid}.json`, exportPayload);
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
