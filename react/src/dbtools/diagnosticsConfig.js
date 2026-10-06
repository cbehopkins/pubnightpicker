import { doc, onSnapshot, setDoc } from "firebase/firestore";
import { db } from "../firebase";

export const DIAGNOSTICS_CONFIG_COLLECTION = "config";
export const DIAGNOSTICS_CONFIG_DOCUMENT = "diagnostics";

function configRef() {
    return doc(db, DIAGNOSTICS_CONFIG_COLLECTION, DIAGNOSTICS_CONFIG_DOCUMENT);
}

export function watchSilenceNotifications(onValue, onError) {
    return onSnapshot(configRef(), { includeMetadataChanges: true }, (snapshot) => {
        if (snapshot.metadata?.hasPendingWrites) return;
        const value = snapshot.data()?.SilenceNotifications;
        if (value !== undefined && typeof value !== "boolean") {
            onError(new Error("SilenceNotifications must be a boolean."));
            return;
        }
        onValue(value ?? false);
    }, onError);
}

export function setSilenceNotifications(value) {
    if (typeof value !== "boolean") throw new TypeError("SilenceNotifications must be a boolean.");
    return setDoc(configRef(), { SilenceNotifications: value }, { merge: true });
}
