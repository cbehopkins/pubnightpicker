// @ts-check

import { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { useSelector } from "react-redux";
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

/**
 * @typedef {Object} VotingListener
 * @property {(payload: unknown) => void} onSnapshot
 * @property {() => void} onReset
 * @property {(message: string) => void} onError
 */

/**
 * @typedef {Object} GroupVotingStream
 * @property {string} key
 * @property {string} groupId
 * @property {Map<string, Set<VotingListener>>} listeners
 * @property {AbortController | null} controller
 * @property {string} connectedMeetKey
 * @property {boolean} scheduled
 */

// One stream per Group and user: browsers allow only six HTTP/1.1 connections per origin.
/** @type {Map<string, GroupVotingStream>} */
const groupVotingStreams = new Map();
const fatalStreamErrorCodes = ["signed_out", "unauthenticated", "forbidden", "not_found"];
const invalidSnapshotMessage = "Last Orders returned an invalid voting snapshot.";
const unavailableMessage = "Voting updates are temporarily unavailable.";

/**
 * @param {string} groupId
 * @param {string} userId
 * @param {string} meetId
 * @param {VotingListener} listener
 * @returns {() => void}
 */
function subscribeGroupVoting(groupId, userId, meetId, listener) {
  const key = `${groupId}\u0000${userId}`;
  let stream = groupVotingStreams.get(key);
  if (!stream) {
    stream = { key, groupId, listeners: new Map(), controller: null, connectedMeetKey: "", scheduled: false };
    groupVotingStreams.set(key, stream);
  }
  const owner = stream;
  let meetListeners = owner.listeners.get(meetId);
  if (!meetListeners) {
    meetListeners = new Set();
    owner.listeners.set(meetId, meetListeners);
  }
  const listeners = meetListeners;
  listeners.add(listener);
  scheduleGroupVoting(owner);
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && owner.listeners.get(meetId) === listeners) owner.listeners.delete(meetId);
    scheduleGroupVoting(owner);
  };
}

/** @param {GroupVotingStream} stream */
function scheduleGroupVoting(stream) {
  if (stream.scheduled) return;
  stream.scheduled = true;
  queueMicrotask(() => {
    stream.scheduled = false;
    const meetIds = [...stream.listeners.keys()].sort();
    const meetKey = meetIds.join("\u0000");
    if (stream.controller && meetKey === stream.connectedMeetKey) return;
    stream.controller?.abort();
    stream.controller = null;
    stream.connectedMeetKey = "";
    if (meetIds.length === 0) {
      if (groupVotingStreams.get(stream.key) === stream) groupVotingStreams.delete(stream.key);
      return;
    }
    startGroupVoting(stream, meetIds, meetKey);
  });
}

/**
 * @param {GroupVotingStream} stream
 * @param {string[]} meetIds
 * @param {(listener: VotingListener) => void} notify
 */
function notifyMeetListeners(stream, meetIds, notify) {
  for (const meetId of meetIds) {
    for (const listener of stream.listeners.get(meetId) ?? []) notify(listener);
  }
}

/**
 * @param {GroupVotingStream} stream
 * @param {string[]} meetIds
 * @param {string} meetKey
 */
function startGroupVoting(stream, meetIds, meetKey) {
  const controller = new AbortController();
  stream.controller = controller;
  stream.connectedMeetKey = meetKey;
  const isCurrent = () => stream.controller === controller && !controller.signal.aborted;
  /** @param {string} message */
  const fail = (message) => notifyMeetListeners(stream, meetIds, (listener) => listener.onError(message));
  const stop = () => {
    if (stream.controller !== controller) return;
    stream.controller = null;
    stream.connectedMeetKey = "";
  };
  try {
    streamLastOrders(`api/meets/${encodeURIComponent(stream.groupId)}/votes/stream`, {
      query: meetIds.map((meetId) => ["meet_id", meetId]),
      signal: controller.signal,
      onopen: async (response) => {
        const contentType = response.headers.get("content-type")?.split(";")[0].trim().toLowerCase();
        if (response.ok && contentType === "text/event-stream") return;
        const code = response.status === 401 ? "unauthenticated" : response.status === 403 ? "forbidden" :
          response.status === 404 ? "not_found" : "stream_unavailable";
        throw new LastOrdersApiError(code, unavailableMessage, { status: response.status });
      },
      onmessage: (event) => {
        if (!isCurrent()) return;
        if (event.event === "view_reset") {
          stop();
          controller.abort();
          notifyMeetListeners(stream, meetIds, (listener) => listener.onReset());
          scheduleGroupVoting(stream);
          return;
        }
        if (event.event !== "voting_snapshots") return;
        let payload;
        try {
          payload = JSON.parse(event.data);
        } catch {
          fail(invalidSnapshotMessage);
          return;
        }
        if (!payload || typeof payload.cursor !== "string" || payload.cursor.length === 0 || !Array.isArray(payload.meets)
          || !payload.meets.every((meet) => meet && typeof meet.meet_id === "string")) {
          fail(invalidSnapshotMessage);
          return;
        }
        for (const meet of payload.meets) {
          for (const listener of stream.listeners.get(meet.meet_id) ?? []) listener.onSnapshot(meet);
        }
      },
      onclose: () => {
        if (!controller.signal.aborted) throw new Error("Voting updates closed unexpectedly.");
      },
      onerror: (error) => {
        if (!isCurrent()) return;
        fail(error?.message || unavailableMessage);
        if (error instanceof LastOrdersApiError && fatalStreamErrorCodes.includes(error.code)) throw error;
        return 5000;
      },
    }).catch((error) => {
      if (!isCurrent()) return;
      stop();
      fail(error?.message || unavailableMessage);
    });
  } catch (error) {
    stop();
    fail(error?.message || unavailableMessage);
  }
}

/** @param {string} pollId @param {string | null} userId @param {boolean} [enabled=true] @returns {VoteState} */
function useVotes(pollId, userId, enabled = true) {
  const groupId = useSelector(
    /** @param {{ group?: { groupId?: string | null } }} state */
    (state) => state.group?.groupId ?? null
  );
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

    void loadSnapshot();
    if (!groupId) {
      return () => {
        active = false;
        snapshotController.abort();
      };
    }
    const unsubscribe = subscribeGroupVoting(groupId, userId, pollId, {
      onSnapshot: (payload) => {
        if (!active) return;
        if (applySnapshot(payload)) streamSequenceRef.current += 1;
      },
      onReset: () => {
        if (!active) return;
        streamSequenceRef.current += 1;
        setOptionsById({});
        setSelection([]);
        setIsLoading(true);
        void loadSnapshot();
      },
      onError: (message) => {
        if (active) setErrorMessage(message);
      },
    });
    return () => {
      active = false;
      snapshotController.abort();
      unsubscribe();
    };
  }, [applySnapshot, enabled, groupId, pollId, setSelection, userId]);

  return { optionsById, voterProfiles, selectedOptionIds, pendingOptionIds, retrySelections, isLoading, errorMessage, changeVote, retryVote };
}

export default useVotes;
