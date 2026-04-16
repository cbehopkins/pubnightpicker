import {
    collection,
    getDocs,
} from "firebase/firestore";
import { db } from "../firebase";

/**
 * Read-only report of users docs that are not stored at canonical users/{uid} paths.
 *
 * USAGE (from browser console):
 * import { countLegacyUserDocs } from "./dbtools/countLegacyUserDocs.js"
 * await countLegacyUserDocs()
 *
 * @param {{ sampleSize?: number }} [options]
 * @returns {Promise<{ totalUsersDocs: number, canonicalDocs: number, legacyUidMismatchDocs: number, missingUidDocs: number, sample: Array<{ docId: string, uid: string | null, reason: string }> }>}
 */
export async function countLegacyUserDocs(options = {}) {
    const sampleSize = Number.isFinite(options.sampleSize) ? Number(options.sampleSize) : 25;

    const result = {
        totalUsersDocs: 0,
        canonicalDocs: 0,
        legacyUidMismatchDocs: 0,
        missingUidDocs: 0,
        sample: [],
    };

    const usersSnapshot = await getDocs(collection(db, "users"));
    result.totalUsersDocs = usersSnapshot.size;

    usersSnapshot.docs.forEach((userDoc) => {
        const data = userDoc.data();
        const uid = typeof data?.uid === "string" && data.uid.length > 0 ? data.uid : null;

        if (uid === null) {
            result.missingUidDocs += 1;
            if (result.sample.length < sampleSize) {
                result.sample.push({
                    docId: userDoc.id,
                    uid: null,
                    reason: "missing uid field",
                });
            }
            return;
        }

        if (userDoc.id === uid) {
            result.canonicalDocs += 1;
            return;
        }

        result.legacyUidMismatchDocs += 1;
        if (result.sample.length < sampleSize) {
            result.sample.push({
                docId: userDoc.id,
                uid,
                reason: "doc id does not match uid",
            });
        }
    });

    console.log("Users docs migration report", result);
    return result;
}
