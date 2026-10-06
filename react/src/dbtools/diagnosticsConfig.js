import { doc, onSnapshot, setDoc } from "firebase/firestore";
import { db } from "../firebase";

export const DIAGNOSTICS_CONFIG_COLLECTION = "config";
export const DIAGNOSTICS_CONFIG_DOCUMENT = "diagnostics";
export const DEFAULT_NOTIFICATION_SETTINGS = {
    SilenceNotifications: false,
    NotifyPollActorWhenSilenced: false,
    KeepChatNotificationsWhenSilenced: false,
};

function configRef() {
    return doc(db, DIAGNOSTICS_CONFIG_COLLECTION, DIAGNOSTICS_CONFIG_DOCUMENT);
}

export function watchNotificationSettings(onValue, onError) {
    return onSnapshot(configRef(), { includeMetadataChanges: true }, (snapshot) => {
        if (snapshot.metadata?.hasPendingWrites) return;
        const data = snapshot.data() ?? {};
        const settings = { ...DEFAULT_NOTIFICATION_SETTINGS };
        for (const field of Object.keys(settings)) {
            if (data[field] !== undefined && typeof data[field] !== "boolean") {
                onError(new Error(`${field} must be a boolean.`));
                return;
            }
            settings[field] = data[field] ?? false;
        }
        onValue(settings);
    }, onError);
}

export function setSilenceNotifications(value) {
    return setNotificationSetting("SilenceNotifications", value);
}

export function watchSilenceNotifications(onValue, onError) {
    return watchNotificationSettings((settings) => onValue(settings.SilenceNotifications), onError);
}

export function setNotificationSetting(field, value) {
    if (!Object.hasOwn(DEFAULT_NOTIFICATION_SETTINGS, field)) throw new TypeError("Unknown notification setting.");
    if (typeof value !== "boolean") throw new TypeError(`${field} must be a boolean.`);
    return setDoc(configRef(), { [field]: value }, { merge: true });
}
