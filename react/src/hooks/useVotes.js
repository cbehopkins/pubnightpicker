// @ts-check

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { LastOrdersApiError, requestLastOrders, streamLastOrders } from "../utils/lastOrdersApi";

/** @typedef {{ uid: string, name?: string, photoUrl?: string }} VoterProfile */
/** @typedef {{ total: number, anonymousCount: number, voterUids: string[], voters: VoterProfile[] }} VotingOption */
/** @typedef {Record<string, VotingOption>} VotingOptionsMap */

/**
 * @typedef {Object} VoteState
 * @property {VotingOptionsMap} optionsById
 * @property {Record<string, VoterProfile>} voterProfiles
 * @property {string[]} selectedOptionIds
 * @property {string[]} pendingOptionIds
 * @property {Record<string, boolean>} retrySelections
 * @property {boolean} isLoading
 * @property {string} errorMessage
 * @property {(optionId: string, selected: boolean) => Promise<boolean>} changeVote
 * @property {(optionId: string) => Promise<boolean>} retryVote
 */

/**
 * @param {unknown} value
 * @returns {{ optionsById: VotingOptionsMap, selectedOptionIds: string[] } | null}
 */
function parseVotingSnapshot(value) {
  if (!value || typeof value !== "object") return null;
  const payload = /** @type {{ options?: unknown, selected_option_ids?: unknown }} */ (value);
  if (!Array.isArray(payload.options) || !Array.isArray(payload.selected_option_ids)) return null;

  /** @type {VotingOptionsMap} */
  const optionsById = {};
  for (const option of payload.options) {
    if (!option || typeof option !== "object") return null;
    const entry = /** @type {{ id?: unknown, total?: unknown, anonymous_count?: unknown, voters?: unknown }} */ (option);
    if (typeof entry.id !== "string" || typeof entry.total !== "number" || !Number.isInteger(entry.total)
      || typeof entry.anonymous_count !== "number" || !Number.isInteger(entry.anonymous_count)
      || !Array.isArray(entry.voters)) return null;
    if (entry.total < 0 || entry.anonymous_count < 0 || entry.anonymous_count > entry.total) return null;
    /** @type {VoterProfile[]} */
    const voters = [];
    for (const voter of entry.voters) {
      if (!voter || typeof voter !== "object" || typeof voter.id !== "string" || !voter.id
        || (voter.name !== undefined && typeof voter.name !== "string")
        || (voter.photo_url !== undefined && typeof voter.photo_url !== "string")) return null;
      voters.push({ uid: voter.id, name: voter.name, photoUrl: voter.photo_url });
    }
    optionsById[entry.id] = {
      total: entry.total,
      anonymousCount: entry.anonymous_count,
      voterUids: voters.map((voter) => voter.uid),
      voters,
    };
  }
  if (!payload.selected_option_ids.every((id) => typeof id === "string")) return null;
  return {
    optionsById,
    selectedOptionIds: [...new Set(payload.selected_option_ids)].sort(),
  };
}

/** @param {string} pollId @param {string | null} userId @param {boolean} [enabled=true] @returns {VoteState} */
function useVotes(pollId, userId, enabled = true) {
  const [optionsById, setOptionsById] = useState(/** @type {VotingOptionsMap} */({}));
  const [selectedOptionIds, setSelectedOptionIds] = useState(/** @type {string[]} */([]));
  const [pendingOptionIds, setPendingOptionIds] = useState(/** @type {string[]} */([]));
  const [retrySelections, setRetrySelections] = useState(/** @type {Record<string, boolean>} */({}));
  const [isLoading, setIsLoading] = useState(Boolean(enabled && userId));
  const [errorMessage, setErrorMessage] = useState("");
  const selectedRef = useRef(/** @type {string[]} */([]));
  const pendingRef = useRef(new Set());
  const retryRef = useRef(/** @type {Record<string, boolean>} */({}));
  const streamSequenceRef = useRef(0);
  const voterProfiles = useMemo(() => Object.fromEntries(
    Object.values(optionsById).flatMap((option) => option.voters.map((voter) => [voter.uid, voter]))
  ), [optionsById]);

  const setSelection = useCallback((next) => {
    selectedRef.current = next;
    setSelectedOptionIds(next);
  }, []);

  const setRetrySelection = useCallback((optionId, selected) => {
    retryRef.current = { ...retryRef.current, [optionId]: selected };
    setRetrySelections(retryRef.current);
  }, []);

  const clearRetrySelection = useCallback((optionId) => {
    const next = { ...retryRef.current };
    delete next[optionId];
    retryRef.current = next;
    setRetrySelections(next);
  }, []);

  const applySnapshot = useCallback((value) => {
    const parsed = parseVotingSnapshot(value);
    if (!parsed) {
      setErrorMessage("Last Orders returned an invalid voting snapshot.");
      return null;
    }
    setOptionsById(parsed.optionsById);
    setSelection(parsed.selectedOptionIds);
    const nextRetrySelections = { ...retryRef.current };
    for (const [optionId, selected] of Object.entries(nextRetrySelections)) {
      if (parsed.selectedOptionIds.includes(optionId) === selected) delete nextRetrySelections[optionId];
    }
    retryRef.current = nextRetrySelections;
    setRetrySelections(nextRetrySelections);
    setErrorMessage("");
    setIsLoading(false);
    return parsed;
  }, [setSelection]);

  const changeVote = useCallback(async (optionId, selected, force = false) => {
    if (!enabled || !userId || !optionId || pendingRef.current.has(optionId)) return false;
    if (!force && selectedRef.current.includes(optionId) === selected) return true;

    pendingRef.current.add(optionId);
    setPendingOptionIds([...pendingRef.current]);
    clearRetrySelection(optionId);
    setSelection(selected
      ? [...selectedRef.current, optionId].sort()
      : selectedRef.current.filter((id) => id !== optionId));

    try {
      await requestLastOrders(`api/meet/${encodeURIComponent(pollId)}/votes/${encodeURIComponent(optionId)}`, {
        method: selected ? "PUT" : "DELETE",
      });
      return true;
    } catch (error) {
      setErrorMessage(error?.message || "Your vote could not be confirmed.");
      const observedSequence = streamSequenceRef.current;
      try {
        const snapshot = await requestLastOrders(`api/meet/${encodeURIComponent(pollId)}/votes`, { method: "GET" });
        const parsed = parseVotingSnapshot(snapshot);
        if (parsed && streamSequenceRef.current === observedSequence) {
          applySnapshot(snapshot);
        }
        if (parsed && parsed.selectedOptionIds.includes(optionId) === selected) {
          clearRetrySelection(optionId);
          return true;
        }
      } catch {
        // Keep the latest requested selection available for an explicit retry.
      }
      setRetrySelection(optionId, selected);
      return false;
    } finally {
      pendingRef.current.delete(optionId);
      setPendingOptionIds([...pendingRef.current]);
    }
  }, [applySnapshot, clearRetrySelection, enabled, pollId, setRetrySelection, setSelection, userId]);

  const retryVote = useCallback(async (optionId) => {
    if (!(optionId in retryRef.current)) return true;
    return changeVote(optionId, retryRef.current[optionId], true);
  }, [changeVote]);

  useEffect(() => {
    selectedRef.current = [];
    retryRef.current = {};
    pendingRef.current.clear();
    streamSequenceRef.current = 0;
    setOptionsById({});
    setSelection([]);
    setPendingOptionIds([]);
    setRetrySelections({});
    setErrorMessage("");
    setIsLoading(Boolean(enabled && userId));
    if (!enabled || !userId) return undefined;

    let active = true;
    const snapshotController = new AbortController();
    /** @type {AbortController | null} */
    let streamController = null;
    const snapshotPath = `api/meet/${encodeURIComponent(pollId)}/votes`;

    const loadSnapshot = async () => {
      const observedSequence = streamSequenceRef.current;
      try {
        const snapshot = await requestLastOrders(snapshotPath, { method: "GET", signal: snapshotController.signal });
        if (active && streamSequenceRef.current === observedSequence) {
          applySnapshot(snapshot);
        }
      } catch (error) {
        if (active && !snapshotController.signal.aborted) {
          setErrorMessage(error?.message || "Voting is temporarily unavailable.");
          setIsLoading(false);
        }
      }
    };

    const connect = () => {
      const controller = new AbortController();
      streamController = controller;
      try {
        streamLastOrders(`api/meet/${encodeURIComponent(pollId)}/votes/stream`, {
          signal: controller.signal,
          onopen: async (response) => {
            const contentType = response.headers.get("content-type")?.split(";")[0].trim().toLowerCase();
            if (response.ok && contentType === "text/event-stream") return;
            const code = response.status === 401 ? "unauthenticated" : response.status === 403 ? "forbidden" : "stream_unavailable";
            throw new LastOrdersApiError(code, "Voting updates are temporarily unavailable.", { status: response.status });
          },
          onmessage: (event) => {
            if (!active || controller.signal.aborted) return;
            if (event.event === "view_reset") {
              streamSequenceRef.current += 1;
              setOptionsById({});
              setSelection([]);
              setIsLoading(true);
              controller.abort();
              void loadSnapshot();
              queueMicrotask(() => {
                if (active && streamController === controller) connect();
              });
              return;
            }
            if (event.event !== "voting_snapshot") return;
            let payload;
            try {
              payload = JSON.parse(event.data);
            } catch {
              setErrorMessage("Last Orders returned an invalid voting snapshot.");
              return;
            }
            if (!payload || typeof payload.cursor !== "string" || payload.cursor.length === 0) {
              setErrorMessage("Last Orders returned an invalid voting snapshot.");
              return;
            }
            const parsed = applySnapshot(payload);
            if (!parsed) return;
            streamSequenceRef.current += 1;
          },
          onclose: () => {
            if (!controller.signal.aborted) throw new Error("Voting updates closed unexpectedly.");
          },
          onerror: (error) => {
            if (!active || controller.signal.aborted) return;
            setErrorMessage(error?.message || "Voting updates are temporarily unavailable.");
            if (error instanceof LastOrdersApiError && ["signed_out", "unauthenticated", "forbidden", "not_found"].includes(error.code)) {
              throw error;
            }
            return 5000;
          },
        }).catch((error) => {
          if (active && !controller.signal.aborted && streamController === controller) {
            setErrorMessage(error?.message || "Voting updates are temporarily unavailable.");
            setIsLoading(false);
          }
        });
      } catch (error) {
        if (active && !controller.signal.aborted) {
          setErrorMessage(error?.message || "Voting updates are temporarily unavailable.");
          setIsLoading(false);
        }
      }
    };

    void loadSnapshot();
    connect();
    return () => {
      active = false;
      snapshotController.abort();
      streamController?.abort();
    };
  }, [applySnapshot, changeVote, enabled, pollId, setSelection, userId]);

  return { optionsById, voterProfiles, selectedOptionIds, pendingOptionIds, retrySelections, isLoading, errorMessage, changeVote, retryVote };
}

export default useVotes;
