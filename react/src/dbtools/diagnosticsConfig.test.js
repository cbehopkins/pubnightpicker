import { beforeEach, expect, it, vi } from "vitest";

const { docMock, snapshotMock, setDocMock, transactionMock } = vi.hoisted(() => ({
    docMock: vi.fn((_db, collection, id) => `${collection}/${id}`), snapshotMock: vi.fn(), setDocMock: vi.fn(), transactionMock: vi.fn(),
}));
vi.mock("firebase/firestore", () => ({ doc: docMock, onSnapshot: snapshotMock, setDoc: setDocMock, runTransaction: transactionMock }));
vi.mock("../firebase", () => ({ db: {} }));
import { DEFAULT_NOTIFICATION_SETTINGS, ensureAdminDeleteConfig, setAdminDeletePaused, setNotificationSetting, setSilenceNotifications, watchAdminDeletePaused, watchNotificationSettings, watchSilenceNotifications } from "./diagnosticsConfig";

beforeEach(() => vi.clearAllMocks());

it("uses the exact document and merge writes", async () => {
    await setSilenceNotifications(true);
    expect(docMock).toHaveBeenCalledWith({}, "config", "diagnostics");
    expect(setDocMock).toHaveBeenCalledWith("config/diagnostics", { SilenceNotifications: true }, { merge: true });
    expect(() => setSilenceNotifications("true")).toThrow(TypeError);
});

it("defaults missing data to false, rejects invalid data, and ignores pending writes", () => {
    const onValue = vi.fn();
    const onError = vi.fn();
    const unsubscribe = vi.fn();
    snapshotMock.mockReturnValue(unsubscribe);
    expect(watchSilenceNotifications(onValue, onError)).toBe(unsubscribe);
    const deliver = snapshotMock.mock.calls[0][2];
    deliver({ data: () => undefined });
    expect(onValue).toHaveBeenLastCalledWith(false);
    deliver({ data: () => ({ SilenceNotifications: true }) });
    expect(onValue).toHaveBeenLastCalledWith(true);
    deliver({ data: () => ({ SilenceNotifications: "false" }) });
    expect(onError).toHaveBeenCalledOnce();
    deliver({ data: () => ({ SilenceNotifications: false }), metadata: { hasPendingWrites: true } });
    expect(onValue).toHaveBeenCalledTimes(2);
});

it("subscribes to an atomic settings value and merge-writes exception flags", async () => {
    const onValue = vi.fn();
    const onError = vi.fn();
    watchNotificationSettings(onValue, onError);
    const deliver = snapshotMock.mock.calls[0][2];
    deliver({ data: () => undefined });
    expect(onValue).toHaveBeenLastCalledWith(DEFAULT_NOTIFICATION_SETTINGS);
    deliver({ data: () => ({ SilenceNotifications: true, KeepChatNotificationsWhenSilenced: true }) });
    expect(onValue).toHaveBeenLastCalledWith({ ...DEFAULT_NOTIFICATION_SETTINGS, SilenceNotifications: true, KeepChatNotificationsWhenSilenced: true });
    deliver({ data: () => ({ NotifyPollActorWhenSilenced: "true" }) });
    expect(onError).toHaveBeenCalledOnce();
    await setNotificationSetting("NotifyPollActorWhenSilenced", true);
    expect(setDocMock).toHaveBeenCalledWith("config/diagnostics", { NotifyPollActorWhenSilenced: true }, { merge: true });
    expect(() => setNotificationSetting("unexpected", true)).toThrow(TypeError);
});

it("initializes missing admin deletion settings without overwriting existing fields or values", async () => {
    const set = vi.fn();
    for (const data of [undefined, { reason: "maintenance" }, { paused: true }, { paused: false }]) {
        transactionMock.mockImplementation((_db, callback) => callback({
            get: async () => ({ data: () => data }), set,
        }));
        set.mockClear();
        await ensureAdminDeleteConfig();
        expect(docMock).toHaveBeenLastCalledWith({}, "config", "admin_delete");
        if (data?.paused === undefined) {
            expect(set).toHaveBeenCalledWith("config/admin_delete", { paused: false }, { merge: true });
        } else {
            expect(set).not.toHaveBeenCalled();
        }
    }
});

it("rejects malformed existing paused values rather than replacing them", async () => {
    const set = vi.fn();
    for (const paused of [null, "false", 0]) {
        transactionMock.mockImplementation((_db, callback) => callback({
            get: async () => ({ data: () => ({ paused }) }), set,
        }));
        await expect(ensureAdminDeleteConfig()).rejects.toThrow("paused must be a boolean");
    }
    expect(set).not.toHaveBeenCalled();
});

it("merge-writes boolean paused values to the specified document", async () => {
    for (const paused of [true, false]) {
        await setAdminDeletePaused(paused);
        expect(setDocMock).toHaveBeenLastCalledWith("config/admin_delete", { paused }, { merge: true });
    }
    expect(() => setAdminDeletePaused("true")).toThrow(TypeError);
});

it("subscribes after initialization, validates confirmed snapshots and cleans up", async () => {
    transactionMock.mockResolvedValue(undefined);
    const unsubscribe = vi.fn();
    snapshotMock.mockReturnValue(unsubscribe);
    const onValue = vi.fn();
    const onError = vi.fn();
    const stop = watchAdminDeletePaused(onValue, onError);
    expect(snapshotMock).not.toHaveBeenCalled();
    await Promise.resolve();
    expect(snapshotMock.mock.calls[0][0]).toBe("config/admin_delete");
    const deliver = snapshotMock.mock.calls[0][2];
    deliver({ data: () => ({ paused: true }), metadata: { hasPendingWrites: true } });
    expect(onValue).not.toHaveBeenCalled();
    deliver({ data: () => ({ paused: true }) });
    deliver({ data: () => ({ paused: false }) });
    expect(onValue.mock.calls).toEqual([[true], [false]]);
    for (const data of [undefined, {}, { paused: "false" }]) deliver({ data: () => data });
    expect(onError).toHaveBeenCalledTimes(3);
    expect(snapshotMock.mock.calls[0][3]).toBe(onError);
    stop();
    expect(unsubscribe).toHaveBeenCalledOnce();
});

it("surfaces initialization errors and does not subscribe after disposal", async () => {
    transactionMock.mockRejectedValueOnce(new Error("Initialization denied"));
    const onError = vi.fn();
    watchAdminDeletePaused(vi.fn(), onError);
    await Promise.resolve();
    expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: "Initialization denied" }));
    expect(snapshotMock).not.toHaveBeenCalled();
    transactionMock.mockResolvedValueOnce(undefined);
    const stop = watchAdminDeletePaused(vi.fn(), onError);
    stop();
    await Promise.resolve();
    expect(snapshotMock).not.toHaveBeenCalled();
});
