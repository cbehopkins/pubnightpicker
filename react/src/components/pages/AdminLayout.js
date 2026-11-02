import { useState } from "react";
import { Accordion } from "react-bootstrap";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import ProtectedRoute from "../ProtectedRoute";

const sections = [
    { path: "/admin/users", label: "Manage Users" },
    { path: "/admin/diagnostics", label: "Diagnostics" },
    { path: "/admin/logs", label: "Logs" },
    { path: "/admin/config", label: "Config" },
];

function AdminLayout() {
    const location = useLocation();
    const { pathname } = location;
    const navigate = useNavigate();
    const [collapsedLocationKey, setCollapsedLocationKey] = useState(null);
    const currentSection = sections.find(({ path }) => pathname === path || pathname.startsWith(`${path}/`));

    return (
        <div className="container-fluid px-3 px-md-4 py-4">
            <h1 className="h5 fw-semibold mb-3">Admin</h1>
            <Accordion
                activeKey={collapsedLocationKey === location.key ? null : currentSection?.path}
                onSelect={(path) => {
                    if (!path) {
                        setCollapsedLocationKey(location.key);
                        return;
                    }
                    setCollapsedLocationKey(null);
                    if (path !== currentSection?.path) {
                        navigate(path);
                    }
                }}
            >
                {sections.map(({ path, label }) => (
                    <Accordion.Item key={path} eventKey={path}>
                        <Accordion.Header>{label}</Accordion.Header>
                        <Accordion.Body>
                            {currentSection?.path === path && <Outlet />}
                        </Accordion.Body>
                    </Accordion.Item>
                ))}
            </Accordion>
        </div>
    );
}

export default ProtectedRoute(AdminLayout, "admin", "/");
