// @ts-check

import { useEffect, useMemo, useState } from "react";
import { NavLink, useSearchParams } from "react-router-dom";
import { Form } from "react-bootstrap";
import ProtectedRoute from "../ProtectedRoute";
import Modal from "../UI/Modal";
import Button from "../UI/Button";
import usePubs from "../../hooks/usePubs";
import useAttendanceVenueStats from "../../hooks/useAttendanceVenueStats";
import { buildVenueCountRows, splitRankedStatRows } from "../../utils/statsRanking";
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
                    <h2 className="h5 mb-2">Configure attendance stats</h2>
                    <p className="text-body-secondary mb-0">Choose how many venues to show and how far back to look.</p>
                </div>

                <div className="row g-3 mb-3">
                    <div className="col-sm-6">
                        <Form.Label htmlFor="attendanceStatsVenueLimit">Venues to show</Form.Label>
                        <Form.Control
                            id="attendanceStatsVenueLimit"
                            type="number"
                            min="1"
                            step="1"
                            value={draftVenueLimit}
                            onChange={(event) => setDraftVenueLimit(event.target.value)}
                        />
                    </div>
                    <div className="col-sm-6">
                        <Form.Label htmlFor="attendanceStatsYearCount">Time window (years)</Form.Label>
                        <Form.Control
                            id="attendanceStatsYearCount"
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

function AttendanceVenueStatsPage() {
    const [searchParams, setSearchParams] = useSearchParams();
    const venueLimit = parsePositiveInteger(searchParams.get("limit"), 5);
    const yearCount = parsePositiveInteger(searchParams.get("years"), 1);
    const [isWindowModalOpen, setIsWindowModalOpen] = useState(false);

    const pubParameters = usePubs();
    const {
        countsByVenueId,
        lastDateByVenueId,
        isLoading,
        startDate,
        endDate,
    } = useAttendanceVenueStats(yearCount);

    const rankedRows = useMemo(
        () => buildVenueCountRows({
            countsByVenueId,
            lastDateByVenueId,
            venues: pubParameters,
        }),
        [countsByVenueId, lastDateByVenueId, pubParameters]
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
                        <h1 className="display-6 fw-bold mb-2">Attendance Venue Stats</h1>
                        <p className="lead text-body-secondary mb-0">
                            Venues ranked by total can-come confirmations in the {getWindowLabel(yearCount)}.
                        </p>
                    </div>

                    <div className="d-flex flex-wrap gap-2">
                        <Button type="button" variant="outline-secondary" onClick={() => setIsWindowModalOpen(true)}>
                            Adjust window
                        </Button>
                        <NavLink className="btn btn-outline-primary" to="/stats/winning_venues">
                            Winning Venue Stats
                        </NavLink>
                    </div>
                </div>

                <div className="mt-3 small text-body-secondary">
                    Showing {venueLimit} venues per list from {startDate} through {endDate}.
                </div>
            </section>

            {isLoading ? (
                <div className="alert alert-secondary mb-0">Loading attendance stats...</div>
            ) : (
                <div className="row g-4">
                    <div className="col-lg-6">
                        <StatRankingList
                            title="Highest attendance confirmations"
                            subtitle="Total can-come confirmations across polls in the chosen period."
                            emptyMessage="No attendance data was found in this time window."
                            items={most}
                        />
                    </div>
                    <div className="col-lg-6">
                        <StatRankingList
                            title="Lowest attendance confirmations"
                            subtitle="Venues with the fewest can-come confirmations in the same period."
                            emptyMessage="No attendance data was found in this time window."
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

export default ProtectedRoute(AttendanceVenueStatsPage, "admin", "/");
