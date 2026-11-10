"""Own subprocess trees without inspecting or terminating unrelated services."""

import ctypes
import os
import secrets
import signal
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import IO


class ProcessTree:
    def __init__(
        self,
        command: list[str],
        *,
        cwd: Path,
        env: dict[str, str],
        stdout: IO[str] | None = None,
        stderr: IO[str] | None = None,
    ) -> None:
        self.job = None
        if os.name == "nt":
            name = "PubNightPickerJourney-" + secrets.token_hex(16)
            self.job = _windows_job(name, assign=False)
            command = [
                sys.executable,
                "-u",
                str(Path(__file__).resolve()),
                name,
                *command,
            ]
        try:
            self.process = subprocess.Popen(
                command,
                cwd=cwd,
                env=env,
                stdin=subprocess.PIPE,
                stdout=stdout,
                stderr=stderr,
                creationflags=(
                    subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0
                ),
                start_new_session=os.name != "nt",
            )
        except BaseException:
            if self.job is not None:
                _stop_windows_job(self.job)
            raise

    def stop(self) -> None:
        process = self.process
        if os.name == "nt":
            if process.poll() is None:
                try:
                    process.send_signal(signal.CTRL_BREAK_EVENT)
                    process.wait(timeout=15)
                except (OSError, subprocess.TimeoutExpired):
                    if process.poll() is None:
                        process.kill()
        else:
            try:
                os.killpg(process.pid, signal.SIGINT)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                pass
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        process.wait(timeout=15)
        if self.job is not None:
            try:
                _stop_windows_job(self.job)
            finally:
                self.job = None
        if process.stdin is not None:
            process.stdin.close()


def _windows_job(name: str, *, assign: bool = True) -> int:
    from ctypes import wintypes

    class BasicLimits(ctypes.Structure):
        _fields_ = [
            ("per_process_user_time", ctypes.c_longlong),
            ("per_job_user_time", ctypes.c_longlong),
            ("limit_flags", wintypes.DWORD),
            ("minimum_working_set", ctypes.c_size_t),
            ("maximum_working_set", ctypes.c_size_t),
            ("active_process_limit", wintypes.DWORD),
            ("affinity", ctypes.c_size_t),
            ("priority_class", wintypes.DWORD),
            ("scheduling_class", wintypes.DWORD),
        ]

    class IOCounters(ctypes.Structure):
        _fields_ = [
            (name, ctypes.c_ulonglong)
            for name in (
                "read_operations",
                "write_operations",
                "other_operations",
                "read_bytes",
                "write_bytes",
                "other_bytes",
            )
        ]

    class ExtendedLimits(ctypes.Structure):
        _fields_ = [
            ("basic", BasicLimits),
            ("io", IOCounters),
            ("process_memory", ctypes.c_size_t),
            ("job_memory", ctypes.c_size_t),
            ("peak_process_memory", ctypes.c_size_t),
            ("peak_job_memory", ctypes.c_size_t),
        ]

    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.CreateJobObjectW.argtypes = [ctypes.c_void_p, wintypes.LPCWSTR]
    kernel.CreateJobObjectW.restype = wintypes.HANDLE
    kernel.SetInformationJobObject.argtypes = [
        wintypes.HANDLE,
        ctypes.c_int,
        ctypes.c_void_p,
        wintypes.DWORD,
    ]
    kernel.SetInformationJobObject.restype = wintypes.BOOL
    kernel.AssignProcessToJobObject.argtypes = [wintypes.HANDLE, wintypes.HANDLE]
    kernel.AssignProcessToJobObject.restype = wintypes.BOOL
    kernel.GetCurrentProcess.restype = wintypes.HANDLE
    job = kernel.CreateJobObjectW(None, name)
    if not job:
        raise ctypes.WinError(ctypes.get_last_error())
    limits = ExtendedLimits()
    limits.basic.limit_flags = 0x2000
    if not kernel.SetInformationJobObject(
        job, 9, ctypes.byref(limits), ctypes.sizeof(limits)
    ):
        raise ctypes.WinError(ctypes.get_last_error())
    if assign and not kernel.AssignProcessToJobObject(job, kernel.GetCurrentProcess()):
        raise ctypes.WinError(ctypes.get_last_error())
    return job


def _stop_windows_job(job: int) -> None:
    from ctypes import wintypes

    class Accounting(ctypes.Structure):
        _fields_ = [
            ("times", ctypes.c_longlong * 4),
            ("page_faults", wintypes.DWORD),
            ("total_processes", wintypes.DWORD),
            ("active_processes", wintypes.DWORD),
            ("terminated_processes", wintypes.DWORD),
        ]

    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.TerminateJobObject.argtypes = [wintypes.HANDLE, wintypes.UINT]
    kernel.TerminateJobObject.restype = wintypes.BOOL
    kernel.QueryInformationJobObject.argtypes = [
        wintypes.HANDLE,
        ctypes.c_int,
        ctypes.c_void_p,
        wintypes.DWORD,
        ctypes.c_void_p,
    ]
    kernel.QueryInformationJobObject.restype = wintypes.BOOL
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel.CloseHandle.restype = wintypes.BOOL
    try:
        if not kernel.TerminateJobObject(job, 1):
            raise ctypes.WinError(ctypes.get_last_error())
        deadline = time.monotonic() + 15
        accounting = Accounting()
        while time.monotonic() < deadline:
            if not kernel.QueryInformationJobObject(
                job, 1, ctypes.byref(accounting), ctypes.sizeof(accounting), None
            ):
                raise ctypes.WinError(ctypes.get_last_error())
            if accounting.active_processes == 0:
                return
            time.sleep(0.01)
        raise TimeoutError(
            "Owned Windows process tree did not terminate within 15 seconds"
        )
    finally:
        kernel.CloseHandle(job)


def _exit_when_parent_closes() -> None:
    sys.stdin.buffer.read()
    os._exit(1)


def _run_child(name: str, command: list[str]) -> None:
    job = _windows_job(name)
    signal.signal(signal.SIGBREAK, lambda signal_number, frame: None)
    threading.Thread(target=_exit_when_parent_closes, daemon=True).start()
    child = subprocess.Popen(command, stdin=subprocess.DEVNULL)
    result = child.wait()
    if job:
        os._exit(ctypes.c_int32(result).value)


if __name__ == "__main__":
    _run_child(sys.argv[1], sys.argv[2:])
