import { useEffect, useState } from "react";
import { doc, getDoc } from "firebase/firestore";
import { db } from "../firebase";

/** @typedef {{ uid: string, name?: string, photoUrl?: string, votesVisible: boolean }} VisibleVoterProfile */

/**
 * @param {string[]} voterUids
 * @returns {Record<string, VisibleVoterProfile>}
 */
function useVisibleVoterProfiles(voterUids) {
  const uidKey = JSON.stringify([...new Set(voterUids.filter((uid) => typeof uid === "string" && uid.length > 0))].sort());
  const [profiles, setProfiles] = useState(/** @type {Record<string, VisibleVoterProfile>} */ ({}));

  useEffect(() => {
    let active = true;
    const uids = /** @type {string[]} */ (JSON.parse(uidKey));
    setProfiles({});

    if (uids.length === 0) return () => { active = false; };

    Promise.all(uids.map(async (uid) => {
      const snapshot = await getDoc(doc(db, "user-public", uid));
      if (!snapshot.exists()) return null;
      const profile = snapshot.data();
      return [uid, {
        uid,
        name: typeof profile.name === "string" ? profile.name : undefined,
        photoUrl: typeof (profile.customPhotoUrl || profile.photoUrl) === "string"
          ? profile.customPhotoUrl || profile.photoUrl
          : undefined,
        votesVisible: profile.votesVisible !== false,
      }];
    })).then((entries) => {
      if (active) setProfiles(Object.fromEntries(entries.filter(Boolean)));
    }).catch(() => {
      if (active) setProfiles({});
    });

    return () => { active = false; };
  }, [uidKey]);

  return profiles;
}

export default useVisibleVoterProfiles;