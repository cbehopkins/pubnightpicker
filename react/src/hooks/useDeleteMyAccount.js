import { useCallback, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useSelector } from "react-redux";
import { getAuth } from "firebase/auth";
import { reauthenticatePasswordUser, deleteCurrentAuthUser } from "../firebase";
import { notifyError, notifyInfo } from "../utils/notify";
import { downloadCurrentUserDataExport } from "../utils/userDataExport";
import { deleteUserAppData, deletionSummaryLines, summarizeDeletionResult } from "../dbtools/userDeletion";

/** @typedef {import("../store").RootState} RootState */

function useDeleteMyAccount() {
    const navigate = useNavigate();
    const uid = useSelector(/** @param {RootState} state */(state) => state.auth.uid);
    const email = useSelector(/** @param {RootState} state */(state) => state.auth.email);
    const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
    const [showTypedConfirm, setShowTypedConfirm] = useState(false);
    const [showReauth, setShowReauth] = useState(false);
    const [isDeleting, setIsDeleting] = useState(false);
    const [isExporting, setIsExporting] = useState(false);
    const [errorString, setErrorString] = useState("");
    const [deleteProgress, setDeleteProgress] = useState("");
    const [deleteSummary, setDeleteSummary] = useState(null);

    const auth = getAuth();
    const providerIds = auth.currentUser?.providerData?.map((provider) => provider.providerId) || [];
    const requiresPasswordReauth = providerIds.includes("password");
    const confirmLabel = email || uid || "your account identifier";
    const expectedConfirmation = String(confirmLabel).trim().toLowerCase();

    const runSelfDelete = useCallback(async () => {
        if (!uid) {
            setErrorString("No active user session found");
            return;
        }

        setIsDeleting(true);
        setDeleteProgress("Removing profile and participation data...");
        try {
            const deletionResult = await deleteUserAppData(uid, { removeRoles: false });
            const summary = summarizeDeletionResult(deletionResult);
            setDeleteSummary(summary);

            setDeleteProgress("Deleting Firebase account...");

            const authDeleteResult = await deleteCurrentAuthUser();
            if (!authDeleteResult.ok) {
                setErrorString(authDeleteResult.message || "Unable to delete this account right now");
                return;
            }

            notifyInfo("Your account and app data were deleted.");
            navigate("/");
        } catch (error) {
            console.error(error);
            setErrorString(error?.message || "Could not complete account deletion");
        } finally {
            setIsDeleting(false);
            setDeleteProgress("");
        }
    }, [navigate, uid]);

    const exportMyData = useCallback(async () => {
        if (!uid) {
            return;
        }

        setIsExporting(true);
        try {
            await downloadCurrentUserDataExport(uid);
        } catch (error) {
            console.error(error);
            notifyError(error?.message || "Unable to export your data.");
        } finally {
            setIsExporting(false);
        }
    }, [uid]);

    const requestDelete = useCallback(() => {
        setShowDeleteConfirm(true);
    }, []);

    const cancelDelete = useCallback(() => {
        setShowDeleteConfirm(false);
    }, []);

    const confirmDeleteStepOne = useCallback(() => {
        setShowDeleteConfirm(false);
        setShowTypedConfirm(true);
    }, []);

    const cancelTypedConfirm = useCallback(() => {
        setShowTypedConfirm(false);
    }, []);

    const typedConfirmHandler = useCallback(async (event, ref) => {
        event.preventDefault();
        const enteredValue = String(ref.current.value || "").trim().toLowerCase();

        if (!enteredValue || enteredValue !== expectedConfirmation) {
            setErrorString(`Type ${confirmLabel} exactly to continue.`);
            return;
        }

        setShowTypedConfirm(false);
        if (requiresPasswordReauth) {
            setShowReauth(true);
            return;
        }

        await runSelfDelete();
    }, [confirmLabel, expectedConfirmation, requiresPasswordReauth, runSelfDelete]);

    const reauthenticateAndDelete = useCallback(async (event, ref) => {
        event.preventDefault();
        const passwordValue = String(ref.current.value || "");

        const reauthResult = await reauthenticatePasswordUser(passwordValue);
        if (!reauthResult.ok) {
            setErrorString(reauthResult.message || "Could not verify your password");
            return;
        }

        setShowReauth(false);
        await runSelfDelete();
    }, [runSelfDelete]);

    const cancelReauth = useCallback(() => {
        setShowReauth(false);
    }, []);

    const clearError = useCallback(() => {
        setErrorString("");
    }, []);

    return {
        confirmLabel,
        showDeleteConfirm,
        showTypedConfirm,
        showReauth,
        isDeleting,
        isExporting,
        errorString,
        deleteProgress,
        deleteSummaryLines: deleteSummary ? deletionSummaryLines(deleteSummary) : [],
        requestDelete,
        cancelDelete,
        confirmDeleteStepOne,
        cancelTypedConfirm,
        typedConfirmHandler,
        reauthenticateAndDelete,
        cancelReauth,
        exportMyData,
        clearError,
    };
}

export default useDeleteMyAccount;
