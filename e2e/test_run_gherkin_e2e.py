"""Local-only safety checks for browser journey orchestration."""

import base64
import json
import os
import socket
import sys
import tempfile
import unittest
from contextlib import ExitStack
from pathlib import Path
from unittest.mock import Mock, patch

import run_gherkin_e2e as runner
from process_owner import ProcessTree


class JourneyRunnerTests(unittest.TestCase):
    def setUp(self) -> None:
        temporary = self.enterContext(tempfile.TemporaryDirectory())
        self.root = Path(temporary)

    def test_environment_discards_inherited_delivery_and_credentials(self) -> None:
        unsafe = {
            "MAILTRAP_TOKEN": "synthetic",
            "MAILTRAP_SANDBOX_TOKEN": "synthetic",
            "SWEEGO_PROVIDER": "synthetic",
            "SWEEGO_BASE_URL": "https://unsafe.invalid",
            "GOOGLE_APPLICATION_CREDENTIALS": "synthetic",
            "LAST_ORDERS_FIREBASE_CREDENTIALS_JSON": "synthetic",
            "WEB_PUSH_VAPID_PRIVATE_KEY": "synthetic",
            "NODE_OPTIONS": "synthetic",
            "LAST_ORDERS_RESET_DB": "true",
            "LANDLORD_INTERNAL_URL": "https://unsafe.invalid",
        }
        with patch.dict(os.environ, unsafe):
            env = runner._environment()
        self.assertTrue(set(unsafe).isdisjoint(env))
        runner._validate_environment(env)

    def test_unsafe_effective_configuration_is_rejected(self) -> None:
        for key, value in (
            ("GOOGLE_CLOUD_PROJECT", "not-demo"),
            ("FIREBASE_AUTH_PROJECT_ID", "not-demo"),
            ("FIRESTORE_EMULATOR_HOST", "remote.invalid:8080"),
            ("FIREBASE_AUTH_EMULATOR_HOST", "remote.invalid:9099"),
            ("VITE_LAST_ORDERS_API_BASE_URL", "https://unsafe.invalid"),
            ("ENABLE_ADMIN_DELETE_REQUESTS", "true"),
            ("ADMIN_DELETE_DRY_RUN", "false"),
            ("SWEEGO_PROVIDER", "synthetic"),
            ("GOOGLE_APPLICATION_CREDENTIALS", "synthetic"),
        ):
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                runner._validate_environment({**runner._environment(), key: value})

    def test_push_uses_one_stable_test_public_key_without_private_credentials(self) -> None:
        with patch.object(runner, "E2E_DIR", self.root), patch.object(
            runner, "_preflight"
        ) as preflight, patch.object(runner, "_discover"), patch.object(
            runner, "_run_group", return_value=0
        ):
            self.assertEqual(runner.main(["--group", "frontend"]), 0)
            self.assertEqual(runner.main(["--group", "frontend"]), 0)
        env: dict[str, str] = preflight.call_args_list[0].args[1]
        next_env: dict[str, str] = preflight.call_args_list[1].args[1]
        public_key = env["VITE_WEB_PUSH_PUBLIC_KEY"]
        decoded = base64.urlsafe_b64decode(public_key + "=" * (-len(public_key) % 4))
        self.assertEqual(env["VITE_ENABLE_WEB_PUSH"], "true")
        self.assertEqual(public_key, next_env["VITE_WEB_PUSH_PUBLIC_KEY"])
        self.assertEqual(len(decoded), 65)
        self.assertEqual(decoded[0], 4)
        self.assertNotIn("WEB_PUSH_VAPID_PRIVATE_KEY", env)

    def test_occupied_port_is_rejected_without_stopping_listener(self) -> None:
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            with self.assertRaisesRegex(RuntimeError, "unavailable"):
                runner._require_free_ports((listener.getsockname()[1],))
            self.assertGreater(listener.fileno(), 0)

    def test_frontend_does_not_build_or_launch_backend(self) -> None:
        with patch.object(runner, "E2E_DIR", self.root), patch.object(
            runner, "_preflight"
        ), patch.object(runner, "_discover"), patch.object(
            runner, "_build_backend"
        ) as build, patch.object(
            runner, "_run_group", return_value=0
        ) as group:
            self.assertEqual(runner.main(["--group", "frontend"]), 0)
        build.assert_not_called()
        self.assertEqual(group.call_args.args[0], "frontend")
        self.assertIsNone(group.call_args.args[1])

    def test_default_runs_both_groups_with_different_state_paths(self) -> None:
        with patch.object(runner, "E2E_DIR", self.root), patch.object(
            runner, "_preflight"
        ), patch.object(runner, "_discover") as discovery, patch.object(
            runner, "_build_backend", return_value=self.root / "backend"
        ) as build, patch.object(
            runner, "_run_group", side_effect=[2, 0]
        ) as group:
            self.assertEqual(runner.main([]), 2)
        build.assert_called_once()
        self.assertEqual(
            [call.args[0] for call in group.call_args_list], ["frontend", "last-orders"]
        )
        self.assertNotEqual(
            group.call_args_list[0].args[2], group.call_args_list[1].args[2]
        )
        self.assertEqual(discovery.call_count, 2)

    def test_failed_backend_build_fails_before_services(self) -> None:
        with patch.object(runner, "_tool", return_value="go"), patch.object(
            runner, "_run_command", return_value=1
        ), self.assertRaisesRegex(RuntimeError, "build failed"):
            runner._build_backend(self.root, runner._environment())

    def test_missing_go_is_not_a_skip(self) -> None:
        def tool(name: str) -> str:
            if name == "go":
                raise FileNotFoundError("go missing")
            return name

        with patch.object(runner, "_tool", side_effect=tool), patch.object(
            runner, "_find_firebase"
        ), patch.object(Path, "is_file", return_value=True), self.assertRaisesRegex(
            FileNotFoundError, "go missing"
        ):
            runner._preflight(("last-orders",), runner._environment())

    def test_discovery_uses_tag_and_rejects_empty_group(self) -> None:
        (self.root / "discovery.json").write_text("[]", encoding="utf-8")
        with patch.object(runner, "_tool", return_value="node"), patch.object(
            runner, "_run_command", return_value=0
        ) as command, self.assertRaisesRegex(RuntimeError, "No scenarios"):
            runner._discover("last-orders", runner._environment(), self.root)
        self.assertIn("@last_orders", command.call_args.args[0])
        self.assertIn("--dry-run", command.call_args.args[0])

    def test_startup_evidence_rejects_provider_or_missing_record(self) -> None:
        path = self.root / "backend.jsonl"
        path.write_text(
            json.dumps({"msg": "firestore production enabled"}) + "\n", encoding="utf-8"
        )
        with self.assertRaisesRegex(RuntimeError, "safety evidence"):
            runner._verify_backend_startup(path)

    def test_malformed_complete_backend_record_is_not_ignored(self) -> None:
        path = self.root / "backend.jsonl"
        path.write_text("not-json\n", encoding="utf-8")
        with self.assertRaises(json.JSONDecodeError):
            runner._verify_backend_startup(path)

    def test_early_backend_exit_stops_waiting_immediately(self) -> None:
        backend = Mock()
        backend.process.poll.return_value = 7
        backend.process.returncode = 7
        with self.assertRaisesRegex(RuntimeError, "Last Orders exited"):
            runner._wait_for_url(runner.BACKEND_URL, {"Last Orders": backend})

    def test_readiness_has_bounded_timeout(self) -> None:
        with patch.object(
            runner.time, "monotonic", side_effect=[0, 61]
        ), self.assertRaises(TimeoutError):
            runner._wait_for_url(runner.BACKEND_URL, {})

    def test_command_timeout_still_stops_owned_process(self) -> None:
        owner = Mock()
        with patch.object(runner, "ProcessTree", return_value=owner), patch.object(
            runner.time, "monotonic", side_effect=[0, 901]
        ), self.assertRaises(TimeoutError):
            runner._run_command(["synthetic"], self.root, runner._environment())
        owner.stop.assert_called_once()

    def test_cleanup_order_and_failure_are_preserved(self) -> None:
        stopped = []
        emulators = Mock()
        emulators.stop.side_effect = lambda: stopped.append("emulators")
        backend = Mock()

        def fail() -> None:
            stopped.append("backend")
            raise OSError("synthetic cleanup failure")

        backend.stop.side_effect = fail
        with patch.object(runner, "_require_free_ports"), self.assertRaisesRegex(
            RuntimeError, "cleanup failure"
        ):
            runner._stop_services({"emulators": emulators, "backend": backend}, ())
        self.assertEqual(stopped, ["backend", "emulators"])

    def test_temporary_cleanup_retries_transient_file_locks(self) -> None:
        with patch.object(
            runner.shutil,
            "rmtree",
            side_effect=[PermissionError("synthetic lock"), None],
        ) as remove, patch.object(runner.time, "sleep"):
            runner._remove_scratch(self.root / "synthetic")
        self.assertEqual(remove.call_count, 2)

    def test_persistent_temporary_file_lock_is_a_cleanup_failure(self) -> None:
        with patch.object(
            runner.shutil,
            "rmtree",
            side_effect=PermissionError("synthetic persistent lock"),
        ), patch.object(
            runner.time, "monotonic", side_effect=[0, 16]
        ), self.assertRaises(
            PermissionError
        ):
            runner._remove_scratch(self.root / "synthetic")

    def test_backend_readiness_failure_cleans_all_started_services(self) -> None:
        owners = [Mock(), Mock()]
        with ExitStack() as stack:
            stack.enter_context(patch.object(runner, "_require_free_ports"))
            stack.enter_context(patch.object(runner, "_remove_stale_hub_locator"))
            stack.enter_context(
                patch.object(runner, "_find_firebase", return_value=["firebase"])
            )
            stack.enter_context(
                patch.object(runner, "_tool", side_effect=lambda name: name)
            )
            stack.enter_context(patch.object(runner, "_wait_for_emulators"))
            stack.enter_context(patch.object(runner, "_run_command", return_value=0))
            stack.enter_context(patch.object(runner, "ProcessTree", side_effect=owners))
            stack.enter_context(
                patch.object(
                    runner, "_wait_for_url", side_effect=TimeoutError("synthetic")
                )
            )
            with self.assertRaises(TimeoutError):
                runner._run_group(
                    "last-orders", self.root / "backend", self.root, self.root
                )
        for owner in owners:
            owner.stop.assert_called_once()

    def test_interruption_during_bootstrap_cleans_emulators(self) -> None:
        owner = Mock()
        with ExitStack() as stack:
            stack.enter_context(patch.object(runner, "_require_free_ports"))
            stack.enter_context(patch.object(runner, "_remove_stale_hub_locator"))
            stack.enter_context(
                patch.object(runner, "_find_firebase", return_value=["firebase"])
            )
            stack.enter_context(
                patch.object(runner, "_tool", side_effect=lambda name: name)
            )
            stack.enter_context(patch.object(runner, "_wait_for_emulators"))
            stack.enter_context(
                patch.object(runner, "_run_command", side_effect=KeyboardInterrupt)
            )
            stack.enter_context(patch.object(runner, "ProcessTree", return_value=owner))
            with self.assertRaises(KeyboardInterrupt):
                runner._run_group("frontend", None, self.root, self.root)
        owner.stop.assert_called_once()

    def test_real_owned_child_exit_code_is_propagated(self) -> None:
        self.assertEqual(
            runner._run_command(
                [sys.executable, "-c", "raise SystemExit(7)"],
                self.root,
                runner._environment(),
            ),
            7,
        )

    @unittest.skipUnless(os.name == "nt", "Windows native exit-code regression")
    def test_native_windows_exit_code_does_not_overflow_wrapper(self) -> None:
        self.assertEqual(
            runner._run_command(
                [sys.executable, "-c", "import os; os._exit(-1073741510)"],
                self.root,
                runner._environment(),
            ),
            3221225786,
        )

    def test_descendant_does_not_survive_launcher_exit(self) -> None:
        child_code = (
            "import socket; server=socket.socket(); server.bind(('127.0.0.1',0));"
            "server.listen(); print(server.getsockname()[1],flush=True); server.accept()"
        )
        parent_code = (
            "import subprocess,sys; "
            f"child=subprocess.Popen([sys.executable,'-c',{child_code!r}],stdout=subprocess.PIPE,text=True); "
            "print(child.stdout.readline(),end='',flush=True)"
        )
        with (self.root / "port.txt").open("w", encoding="utf-8") as output:
            owner = ProcessTree(
                [sys.executable, "-c", parent_code],
                cwd=self.root,
                env=runner._environment(),
                stdout=output,
            )
            try:
                self.assertEqual(owner.process.wait(timeout=15), 0)
            finally:
                owner.stop()
        port = int((self.root / "port.txt").read_text(encoding="utf-8"))
        runner._require_free_ports((port,))


if __name__ == "__main__":
    unittest.main()
