// @ts-check

import { useEffect, useMemo, useState } from "react";
import { NavLink, useSearchParams } from "react-router-dom";
import { Form } from "react-bootstrap";
import ProtectedRoute from "../ProtectedRoute";
import Modal from "../UI/Modal";
import Button from "../UI/Button";
import usePubs from "../../hooks/usePubs";
import useWinningVenueStats from "../../hooks/useWinningVenueStats";
import { buildWinningVenueRows, splitRankedStatRows } from "../../utils/statsRanking";
import StatRankingList from "../UI/StatRankingList";

function parsePositiveInteger(value, fallback) {
    const parsedValue = Number.parseInt(value || "", 10);
    return Number.isFinite(parsedValue) && parsedValue > 0 ? parsedValue : fallback;
}

function getWindowLabel(yearCount) {
    return yearCount === 1 ? "last year" : `last ${yearCount} years`;
}

/**
 * @param {{
 *   yearCount: number,
 *   venueLimit: number,
 *   onClose: () => void,
 *   onSave: (nextVenueLimit: number, nextYearCount: number) => void,
 * }} props
 */
function StatsWindowModal({ yearCount, venueLimit, onClose, onSave }) {
    const [draftVenueLimit, setDraftVenueLimit] = useState(String(venueLimit));
    const [draftYearCount, setDraftYearCount] = useState(String(yearCount));

    useEffect(() => {
        setDraftVenueLimit(String(venueLimit));
        setDraftYearCount(String(yearCount));
    }, [venueLimit, yearCount]);

    return (
        <Modal onBackdropClick={onClose}>
            <div className="bg-body text-body rounded shadow-sm border p-3 p-md-4">
                <div className="mb-3">
                    <h2 className="h5 mb-2">Configure winning venue stats</h2>
                    <p className="text-body-secondary mb-0">Choose how many venues to show and how far back to look.</p>
                </div>

                <div className="row g-3 mb-3">
                    <div className="col-sm-6">
                        <Form.Label htmlFor="statsVenueLimit">Venues to show</Form.Label>
                        <Form.Control
                            id="statsVenueLimit"
                            type="number"
                            min="1"
                            step="1"
                            value={draftVenueLimit}
                            onChange={(event) => setDraftVenueLimit(event.target.value)}
                        />
                    </div>
                    <div className="col-sm-6">
                        <Form.Label htmlFor="statsYearCount">Time window (years)</Form.Label>
                        <Form.Control
                            id="statsYearCount"
                            type="number"
                            min="1"
                            step="1"
                            value={draftYearCount}
                            onChange={(event) => setDraftYearCount(event.target.value)}
                        />
                    </div>
                </div>

                <div className="d-flex flex-wrap justify-content-end gap-2">
                    <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
                    <Button
                        type="button"
                        onClick={() => {
                            onSave(
                                parsePositiveInteger(draftVenueLimit, venueLimit),
                                parsePositiveInteger(draftYearCount, yearCount)
                            );
                        }}
                    >
                        Apply
                    </Button>
                </div>
            </div>
        </Modal>
    );
}

function WinningVenueStatsPage() {
    const [searchParams, setSearchParams] = useSearchParams();
    const venueLimit = parsePositiveInteger(searchParams.get("limit"), 5);
    const yearCount = parsePositiveInteger(searchParams.get("years"), 1);
    const [isWindowModalOpen, setIsWindowModalOpen] = useState(false);

    const pubParameters = usePubs();
    const { polls, isLoading, startDate, endDate } = useWinningVenueStats(yearCount);

    const rankedRows = useMemo(
        () => buildWinningVenueRows({ polls, venues: pubParameters }),
        [polls, pubParameters]
    );
    const { most, least } = useMemo(
        () => splitRankedStatRows(rankedRows, venueLimit),
        [rankedRows, venueLimit]
    );

    const updateSearchParams = (nextVenueLimit, nextYearCount) => {
        const nextParams = new URLSearchParams(searchParams);
        nextParams.set("limit", String(nextVenueLimit));
        nextParams.set("years", String(nextYearCount));
        setSearchParams(nextParams, { replace: true });
        setIsWindowModalOpen(false);
    };

    return (
        <div className="container py-4 py-md-5">
            <section className="mb-4">
                <div className="d-flex flex-wrap align-items-start justify-content-between gap-3">
                    <div>
                        <h1 className="display-6 fw-bold mb-2">Winning Venue Stats</h1>
                        <p className="lead text-body-secondary mb-0">
                            Venues selected from completed polls during the {getWindowLabel(yearCount)}.
                        </p>
                    </div>

                    <div className="d-flex flex-wrap gap-2">
                        <Button type="button" variant="outline-secondary" onClick={() => setIsWindowModalOpen(true)}>
                            Adjust window
                        </Button>
                        <NavLink className="btn btn-outline-primary" to="/stats/attendance">
                            Attendance Stats
                        </NavLink>
                        <NavLink className="btn btn-outline-primary" to="/past_events">
                            Back to Past Events
                        </NavLink>
                    </div>
                </div>

                <div className="mt-3 small text-body-secondary">
                    Showing {venueLimit} venues per list from {startDate} through {endDate}.
                </div>
            </section>

            {isLoading ? (
                <div className="alert alert-secondary mb-0">Loading winning venue stats...</div>
            ) : (
                <div className="row g-4">
                    <div className="col-lg-6">
                        <StatRankingList
                            title="Most often selected"
                            subtitle="Winning venues ranked from highest to lowest in the chosen period."
                            emptyMessage="No completed polls were found in this time window."
                            items={most}
                        />
                    </div>
                    <div className="col-lg-6">
                        <StatRankingList
                            title="Least often selected"
                            subtitle="Venues that rarely or never won during the same window."
                            emptyMessage="No completed polls were found in this time window."
                            items={least}
                        />
                    </div>
                </div>
            )}

            {isWindowModalOpen && (
                <StatsWindowModal
                    venueLimit={venueLimit}
                    yearCount={yearCount}
                    onClose={() => setIsWindowModalOpen(false)}
                    onSave={updateSearchParams}
                />
            )}
        </div>
    );
}

export default ProtectedRoute(WinningVenueStatsPage, "admin", "/");
