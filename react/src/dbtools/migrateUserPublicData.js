import {
    collection,
    getDocs,
    doc,
    setDoc,
} from "firebase/firestore";
import { db } from "../firebase";

/**
 * One-time migration: Copy user profile data from users collection to user-public collection.
 * This extracts name and photoUrl from existing users/{uid} documents and writes to user-public/{uid}.
 * 
 * USAGE (from browser console):
 * import { migrateUserPublicData } from './dbtools/migrateUserPublicData.js'
 * await migrateUserPublicData()
 * 
 * @returns {Promise<Object>} Migration result with counts of processed, skipped, and error docs
 */
export async function migrateUserPublicData() {
    console.log("Starting user-public data migration...");

    const result = {
        processed: 0,
        skipped: 0,
        errors: 0,
        failedDocs: [],
    };

    try {
        // Fetch all documents from users collection
        const usersCollection = collection(db, "users");
        const usersSnapshot = await getDocs(usersCollection);

        console.log(`Found ${usersSnapshot.docs.length} users to migrate.`);

        // Process each user document
        for (const userDoc of usersSnapshot.docs) {
            try {
                const userData = userDoc.data();
                const uid = typeof userData?.uid === "string" && userData.uid.length > 0
                    ? userData.uid
                    : userDoc.id;

                // Extract only public fields (name, photoUrl)
                const publicData = { uid };
                if (userData.name) {
                    publicData.name = userData.name;
                }
                if (userData.photoUrl) {
                    publicData.photoUrl = userData.photoUrl;
                }

                // Skip if no public data to migrate
                if (Object.keys(publicData).length === 0) {
                    result.skipped += 1;
                    continue;
                }

                // Write to user-public/{uid}
                const userPublicDocRef = doc(db, "user-public", uid);
                await setDoc(userPublicDocRef, publicData, { merge: true });

                result.processed += 1;
                console.log(`✓ Migrated user ${uid}`);
            } catch (err) {
                result.errors += 1;
                result.failedDocs.push({
                    uid: userDoc.id,
                    error: err.message,
                });
                console.error(`✗ Error migrating user ${userDoc.id}:`, err);
            }
        }

        console.log("\n=== MIGRATION COMPLETE ===");
        console.log(`Processed: ${result.processed}`);
        console.log(`Skipped: ${result.skipped}`);
        console.log(`Errors: ${result.errors}`);
        if (result.failedDocs.length > 0) {
            console.error("Failed migrations:", result.failedDocs);
        }

        return result;
    } catch (err) {
        console.error("Migration failed with error:", err);
        throw err;
    }
}
