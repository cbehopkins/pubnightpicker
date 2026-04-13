import { useEffect, useState } from "react";
import { doc, onSnapshot } from "firebase/firestore";
import { db } from "../firebase";

/**
 * Fetch private user data from the users collection (restricted access).
 * Admins can read other users' private data via Firestore rules.
 * Non-admins can read their own private data.
 * Returns null if userId is null/undefined.
 * 
 * @param {string|null} userId - The UID of the user to fetch private data for
 * @returns {Object|null} Private user data (email, notification settings, etc.) or null if not loaded
 */
export default function useUserPrivateData(userId) {
    const [privateData, setPrivateData] = useState(null);

    useEffect(() => {
        // Skip if no userId provided
        if (!userId) {
            setPrivateData(null);
            return;
        }

        // Subscribe to user's private data document
        const userDocRef = doc(db, "users", userId);
        const unsubscribe = onSnapshot(
            userDocRef,
            (snapshot) => {
                if (snapshot.exists()) {
                    setPrivateData(snapshot.data());
                } else {
                    setPrivateData(null);
                }
            },
            (error) => {
                console.error(`Error fetching private data for user ${userId}:`, error);
                setPrivateData(null);
            }
        );

        // Cleanup subscription on unmount or when userId changes
        return () => {
            unsubscribe();
        };
    }, [userId]);

    return privateData;
}
