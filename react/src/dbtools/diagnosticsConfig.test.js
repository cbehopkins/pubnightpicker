import { beforeEach, expect, it, vi } from "vitest";

const { docMock, snapshotMock, setDocMock } = vi.hoisted(() => ({
    docMock: vi.fn(() => "config/diagnostics"), snapshotMock: vi.fn(), setDocMock: vi.fn(),
}));
vi.mock("firebase/firestore", () => ({ doc: docMock, onSnapshot: snapshotMock, setDoc: setDocMock }));
vi.mock("../firebase", () => ({ db: {} }));
import { DEFAULT_NOTIFICATION_SETTINGS, setNotificationSetting, setSilenceNotifications, watchNotificationSettings, watchSilenceNotifications } from "./diagnosticsConfig";

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
