// @ts-check

import { useCallback, useState } from "react";
import PollVote from "./PollVote";
import VenueOptions from "./VenueOptions";
import VenueFilter, { antiVenueParams } from "./VenueFilter";
import Button from "./Button";
import AutopopulateButton from "./AutopopulateButton";
import styles from "./ActivePoll.module.css";
import useRole from "../../hooks/useRole";
import useOnlineStatus from "../../hooks/useOnlineStatus";
import useAutopopulateVenueSelector from "../../hooks/useAutopopulateVenueSelector";
import useAutopopulateAction from "../../hooks/useAutopopulateAction";
import { addVenueToMeet, deleteMeet } from "../../dbtools/polls";
import { NOTIFICATION_PING_TIMEOUT_MS } from "../../constants/notificationPing";
import { DEFAULT_VENUE_TYPE, VENUE_TYPE_OPTIONS, getVenueTypeLabel } from "../../constants/venueTypes";
import { getUserFacingErrorMessage } from "../../permissions";
import { notifyError } from "../../utils/notify";
import NotificationPingStatus from "./NotificationPingStatus";
import MeetChatModal from "../pages/MeetChatModal";

/** @typedef {"all" | "pub" | "restaurant" | "event"} VenueType */

/** @typedef {{ name?: string, venueType?: string, [key: string]: unknown }} VenueParametersEntry */
/** @typedef {Record<string, VenueParametersEntry | undefined>} VenueParametersMap */
/** @typedef {Record<string, boolean>} FilterMap */
/** @typedef {{ venues?: Record<string, unknown>, date?: string }} MeetData */

/**
 * @param {FilterMap} anObject
 * @returns {boolean}
 */
function anyEntryTrue(anObject) {
    for (const [, value] of Object.entries(anObject)) {
        if (value) {
            return true;
        }
        return false;
    }

    return false;
}

/**
 * @param {VenueParametersMap} venues
 * @param {Record<string, unknown> | null | undefined} currentVenues
 * @param {FilterMap} venueFilters
 * @param {FilterMap} venueAntiFilters
 * @param {string} venueTypeFilter
 * @returns {VenueParametersMap}
 */
function filterVenueList(venues, currentVenues, venueFilters, venueAntiFilters, venueTypeFilter) {
    const allAvailableVenues = new Set(Object.keys(venues || {}));
    const currentlySelectedVenues = new Set(Object.keys(currentVenues || {}));
    const availableVenues = [...allAvailableVenues].filter((venueId) => {
        if (currentlySelectedVenues.has(venueId)) {
            return false;
        }

        if (venueTypeFilter !== "all") {
            const venueType = venues[venueId]?.venueType || DEFAULT_VENUE_TYPE;
            if (venueType !== venueTypeFilter) {
                return false;
            }
        }

        const venueParams = venues[venueId] || {};
        for (const [key, value] of Object.entries(venueFilters)) {
            if (value) {
                if (!(Object.hasOwn(venueParams, key) && venueParams[key])) {
                    return false;
                }
            }
        }

        if (!anyEntryTrue(venueAntiFilters)) {
            return true;
        }

        for (const [key, value] of Object.entries(venueAntiFilters)) {
            if (value) {
                if (Object.hasOwn(venueParams, key) && venueParams[key]) {
                    return false;
                }
            }
        }

        return true;
    });

    const filteredVenues = availableVenues.reduce((object, key) => {
        object[key] = venues[key];
        return object;
    }, /** @type {VenueParametersMap} */({}));

    return filteredVenues;
}

/**
 * @param {{
 *  poll_id: string,
 *  venues: VenueParametersMap,
 *  poll_data: MeetData,
 *  on_complete: (venueId: string, venueName: string, pollId: string) => void,
 *  mobile?: boolean,
 * }} props
 */
function ActivePoll({ poll_id, venues, poll_data, on_complete, mobile }) {
    const canDeletePoll = useRole("canCreatePoll");
    const canAddVenue = useRole("canAddPubToPoll");
    const canChat = useRole("canChat");
    const { isOnline } = useOnlineStatus();
    const [isChatOpen, setIsChatOpen] = useState(false);

    // Load autopopulate venue suggestions
    const {
        mostVisited,
        leastVisited,
        random: randomVenues,
        isLoading: isAutopopulateLoading,
    } = useAutopopulateVenueSelector(poll_id, poll_data.venues);
    const isAutopopulateEnabled = mostVisited.length > 0 || leastVisited.length > 0 || randomVenues.length > 0;
    /** @type {[FilterMap, import("react").Dispatch<import("react").SetStateAction<FilterMap>>]} */
    const [venueFilters, setVenueFilters] = useState({});
    /** @type {[FilterMap, import("react").Dispatch<import("react").SetStateAction<FilterMap>>]} */
    const [venueAntiFilters, setVenueAntiFilters] = useState({});
    /** @type {[VenueType, import("react").Dispatch<import("react").SetStateAction<VenueType>>]} */
    const [venueTypeFilter, setVenueTypeFilter] = useState("all");

    const [selectedVenue, setSelectedVenue] = useState("");
    const [manualAddWarning, setManualAddWarning] = useState("");
    /** @type {(event: import("react").ChangeEvent<HTMLSelectElement>) => void} */
    const selectVenueHandler = useCallback(
        (event) => {
            event.preventDefault();
            setSelectedVenue(event.target.value);
            setManualAddWarning("");
        },
        [setSelectedVenue, setManualAddWarning]
    );

    /** @type {(event: import("react").MouseEvent<HTMLButtonElement>) => Promise<void>} */
    const addNewVenueToPoll = useCallback(
        async (event) => {
            event.preventDefault();
            try {
                await addVenueToMeet(selectedVenue, poll_id, venues, poll_data.date);
                if (venues[selectedVenue]?.banned) {
                    const selectedVenueName = venues[selectedVenue]?.name || "Selected venue";
                    setManualAddWarning(`${selectedVenueName} is marked as banned. Please only keep it in the poll if you really need to.`);
                    return;
                }

                setManualAddWarning("");
            } catch (error) {
                notifyError(getUserFacingErrorMessage(error, "Unable to add the venue to this poll."));
            }
        },
        [selectedVenue, poll_id, venues, poll_data.date]
    );

    const { handleAutopopulate } = useAutopopulateAction({
        pollId: poll_id,
        pollDate: poll_data.date,
        pollVenues: poll_data.venues,
        venueParameters: venues,
        mostVisited,
        leastVisited,
        randomVenues,
    });

    /** @type {(event: import("react").MouseEvent<HTMLButtonElement>) => Promise<void>} */
    const deletePollHandler = useCallback(
        async (event) => {
            event.preventDefault();
            try {
                await deleteMeet(poll_id);
            } catch (error) {
                notifyError(getUserFacingErrorMessage(error, "Unable to delete this poll."));
            }
        },
        [poll_id]
    );

    const filteredVenueOptions = filterVenueList(
        venues,
        poll_data.venues,
        venueFilters,
        venueAntiFilters,
        venueTypeFilter
    );

    const styleToUse = mobile ? styles.poll_mobile : styles.poll;

    return (
        <>
            <div className={styleToUse}>
                <div className="d-flex align-items-center justify-content-between gap-2 mb-2">
                    <h2 className="mb-0">{poll_data.date}</h2>
                    {canChat && (
                        <Button
                            type="button"
                            variant="outline-secondary"
                            className="btn-sm"
                            onClick={() => setIsChatOpen(true)}
                        >
                            Meet Chat
                        </Button>
                    )}
                </div>
                {canAddVenue && (
                    <VenueFilter
                        title="Filter venue list to contain only items that have:"
                        setVenueFilters={setVenueFilters}
                    />
                )}
                {canAddVenue && (
                    <VenueFilter
                        title="Filter venue list to contain only items that do not have:"
                        setVenueFilters={setVenueAntiFilters}
                        venueParams={antiVenueParams}
                    />
                )}
                {canAddVenue && (
                    <div className={styles.filterRow}>
                        <label htmlFor={`venue-type-filter-${poll_id}`}>Filter by venue type:</label>
                        <select
                            id={`venue-type-filter-${poll_id}`}
                            value={venueTypeFilter}
                            /** @param {import("react").ChangeEvent<HTMLSelectElement>} event */
                            onChange={(event) => {
                                setVenueTypeFilter(/** @type {VenueType} */(event.target.value));
                            }}
                        >
                            {VENUE_TYPE_OPTIONS.map((venueType) => {
                                return (
                                    <option key={venueType} value={venueType}>
                                        {getVenueTypeLabel(venueType)}
                                    </option>
                                );
                            })}
                        </select>
                    </div>
                )}
                <div>
                    {canDeletePoll && (
                        <Button
                            type="button"
                            variant="danger"
                            className={styles["button--alt"]}
                            onClick={deletePollHandler}
                            title={isOnline ? "Delete this entire poll and all associated voting data" : "Unavailable offline"}
                            disabled={!isOnline}
                        >
                            Delete Poll
                        </Button>
                    )}
                    {canAddVenue && (
                        <>
                            <VenueOptions venues={filteredVenueOptions} selectVenueHandler={selectVenueHandler} />
                            <Button type="button" onClick={addNewVenueToPoll}>
                                Add Venue To Poll
                            </Button>
                            {manualAddWarning && (
                                <div className="alert alert-warning mt-2 py-2" role="alert">
                                    {manualAddWarning}
                                </div>
                            )}
                            <AutopopulateButton
                                isLoading={isAutopopulateLoading}
                                isEnabled={isAutopopulateEnabled}
                                onAutopopulate={handleAutopopulate}
                            />
                        </>
                    )}
                    {canDeletePoll && (
                        <NotificationPingStatus
                            documentId={poll_id}
                            eventKey="create"
                            timeoutMs={NOTIFICATION_PING_TIMEOUT_MS}
                        />
                    )}
                </div>
                <PollVote
                    poll_data={poll_data}
                    poll_id={poll_id}
                    on_complete={on_complete}
                />
            </div>

            {isChatOpen && (
                <MeetChatModal meetId={poll_id} onClose={() => setIsChatOpen(false)} />
            )}
        </>
    );
}

export default ActivePoll;
