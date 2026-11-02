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
    const { pathname } = useLocation();
    const navigate = useNavigate();
    const currentSection = sections.find(({ path }) => pathname === path || pathname.startsWith(`${path}/`));

    return (
        <div className="container-fluid px-3 px-md-4 py-4">
            <h1 className="h5 fw-semibold mb-3">Admin</h1>
            <Accordion
                activeKey={currentSection?.path}
                onSelect={(path) => {
                    if (path && path !== currentSection?.path) {
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
