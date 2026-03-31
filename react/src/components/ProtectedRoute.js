import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import useRole from "../hooks/useRole";

/**
 * Higher-Order Component that protects a page component with permission checking
 * Automatically redirects to a fallback route if user lacks required permission
 * 
 * @param {React.ComponentType} Component - Component to protect
 * @param {string} requiredPermission - Permission identifier to check (e.g., "canManagePubs")
 * @param {string} redirectTo - Route to navigate to if unauthorized (default: "/")
 * @returns {React.ComponentType} Wrapped component with permission guard
 * 
 * @example
 * export default ProtectedRoute(EditPubPage, "canManagePubs", "/");
 */
export function ProtectedRoute(Component, requiredPermission, redirectTo = "/") {
  return function ProtectedComponent(props) {
    const navigate = useNavigate();
    const hasPermission = useRole(requiredPermission);

    useEffect(() => {
      if (!hasPermission) {
        navigate(redirectTo);
      }
    }, [hasPermission, navigate]);

    // If no permission, don't render component; let redirect handle it
    if (!hasPermission) {
      return null;
    }

    return <Component {...props} />;
  };
}

export default ProtectedRoute;
