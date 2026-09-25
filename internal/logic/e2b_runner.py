"""Host-side adapter for the official E2B code interpreter SDK."""

import json
import os
import sys


RESULT_PREFIX = "CR_AGENT_E2B_RESULT:"
TEST_RESULT_PREFIX = "CR_AGENT_TEST_RESULT:"
MAX_OUTPUT_CHARS = 6000


def _tail(value):
    value = value or ""
    if len(value) <= MAX_OUTPUT_CHARS:
        return value
    return "[output truncated]\n" + value[-MAX_OUTPUT_CHARS:]


def _test_program(archive_path, commands, timeout_seconds):
    lines = [
        "import json, os, shutil, stat, subprocess, tempfile, time, zipfile",
        "from pathlib import PurePosixPath",
        "import signal, threading",
        f"archive_path = {json.dumps(archive_path)}",
        f"commands = json.loads({json.dumps(json.dumps(commands))})",
        f"timeout_seconds = {int(timeout_seconds)}",
        f"result_prefix = {json.dumps(TEST_RESULT_PREFIX)}",
        "started = time.monotonic()",
        "ran = False",
        "timed_out = False",
        "runner_error = ''",
        "outputs = []",
        "max_stream_bytes = 6000",
        "def drain_stream(stream):",
        "    tail = bytearray()",
        "    while True:",
        "        chunk = stream.read(8192)",
        "        if not chunk:",
        "            break",
        "        tail.extend(chunk)",
        "        if len(tail) > max_stream_bytes:",
        "            del tail[:len(tail) - max_stream_bytes]",
        "    return tail.decode('utf-8', errors='replace')",
        "root = tempfile.mkdtemp(prefix='cr-agent-review-')",
        "try:",
        "    with zipfile.ZipFile(archive_path) as source:",
        "        for item in source.infolist():",
        "            relative = PurePosixPath(item.filename)",
        "            if relative.is_absolute() or any(part in ('', '.', '..') for part in relative.parts):",
        "                raise ValueError('unsafe source path in normalized archive')",
        "            target = os.path.realpath(os.path.join(root, *relative.parts))",
        "            if os.path.commonpath((root, target)) != root:",
        "                raise ValueError('source path escaped sandbox workspace')",
        "            mode = item.external_attr >> 16",
        "            if stat.S_ISLNK(mode):",
        "                raise ValueError('symbolic links are not allowed in source archive')",
        "            if item.is_dir():",
        "                os.makedirs(target, exist_ok=True)",
        "                continue",
        "            os.makedirs(os.path.dirname(target), exist_ok=True)",
        "            with source.open(item) as source_file, open(target, 'wb') as target_file:",
        "                shutil.copyfileobj(source_file, target_file)",
        "            os.chmod(target, mode & 0o777)",
        "    home = os.path.join(root, '.sandbox-home')",
        "    os.makedirs(home, exist_ok=True)",
        "    test_env = {'PATH': os.environ.get('PATH', ''), 'HOME': home, 'CI': 'true', 'GOTOOLCHAIN': 'local'}",
        "    for command in commands:",
        "        remaining = timeout_seconds - (time.monotonic() - started)",
        "        if remaining <= 0:",
        "            timed_out = True",
        "            break",
        "        argv = command.get('args') or []",
        "        if not argv or any(not isinstance(part, str) for part in argv):",
        "            runner_error = 'invalid host-selected test command'",
        "            break",
        "        try:",
        "            process = subprocess.Popen(argv, cwd=root, env=test_env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)",
        "            stdout_value = []",
        "            stderr_value = []",
        "            stdout_thread = threading.Thread(target=lambda: stdout_value.append(drain_stream(process.stdout)), daemon=True)",
        "            stderr_thread = threading.Thread(target=lambda: stderr_value.append(drain_stream(process.stderr)), daemon=True)",
        "            stdout_thread.start()",
        "            stderr_thread.start()",
        "            try:",
        "                exit_code = process.wait(timeout=max(1, remaining))",
        "            except subprocess.TimeoutExpired:",
        "                try:",
        "                    os.killpg(process.pid, signal.SIGKILL)",
        "                except ProcessLookupError:",
        "                    pass",
        "                process.wait()",
        "                exit_code = 124",
        "                timed_out = True",
        "            try:",
        "                os.killpg(process.pid, signal.SIGKILL)",
        "            except ProcessLookupError:",
        "                pass",
        "            stdout_thread.join()",
        "            stderr_thread.join()",
        "            ran = True",
        "            outputs.append({'name': command.get('name', 'test'), 'exit_code': exit_code, 'stdout': stdout_value[0] if stdout_value else '', 'stderr': stderr_value[0] if stderr_value else ''})",
        "            if timed_out:",
        "                break",
        "        except FileNotFoundError:",
        "            runner_error = 'required test runtime is not installed in the sandbox template: ' + str(argv[0])",
        "            break",
        "except Exception as error:",
        "    runner_error = 'sandbox checkout or test runner failed: ' + type(error).__name__",
        "finally:",
        "    shutil.rmtree(root, ignore_errors=True)",
        "def is_collection_error(item):",
        "    output = (item.get('stdout', '') + item.get('stderr', '')).lower()",
        "    is_python_exit = item.get('name') == 'Python' and item.get('exit_code') == 2",
        "    return is_python_exit and 'errors during collection' in output",
        "collection_failed = any(is_collection_error(item) for item in outputs)",
        "test_failure = any(item['exit_code'] != 0 and not is_collection_error(item) for item in outputs)",
        "if runner_error or timed_out:",
        "    status = 'incomplete'",
        "elif collection_failed:",
        "    status = 'incomplete'",
        "elif test_failure:",
        "    status = 'failed'",
        "elif ran:",
        "    status = 'passed'",
        "else:",
        "    status = 'incomplete'",
        "collection_only = collection_failed and len(outputs) == 1 and is_collection_error(outputs[0])",
        "summary = {",
        "    'ran': ran and not collection_only,",
        "    'status': status,",
        "    'runner_error': runner_error,",
        "    'timed_out': timed_out,",
        "    'collection_failed': collection_failed,",
        "    'commands': outputs,",
        "}",
        "print(result_prefix + json.dumps(summary, ensure_ascii=False))",
    ]
    return "\n".join(lines)


def _read_remote_result(lines):
    for line in reversed(lines):
        line = line.strip()
        if line.startswith(TEST_RESULT_PREFIX):
            return json.loads(line[len(TEST_RESULT_PREFIX) :])
    return None


def main():
    if len(sys.argv) != 4:
        raise ValueError("runner arguments are invalid")
    archive_path = sys.argv[1]
    commands = json.loads(sys.argv[2])
    timeout_seconds = int(sys.argv[3])
    from e2b_code_interpreter import Sandbox

    sandbox = None
    response = {
        "ran": False,
        "status": "incomplete",
        "message": "E2B SDK 调用失败，自动化测试未能完成。",
        "output": "",
    }
    streamed_stdout = []
    streamed_stderr = []
    try:
        template = os.environ.get("AGS_TEMPLATE", "").strip() or os.environ.get("E2B_TEMPLATE", "").strip()
        if not template:
            raise ValueError("sandbox template is missing")
        sandbox_timeout = min(timeout_seconds + 120, 3600)
        sandbox = Sandbox.create(template=template, timeout=sandbox_timeout)
        with open(archive_path, "rb") as archive_file:
            sandbox.files.write("/tmp/cr-agent-source.zip", archive_file)
        execution = sandbox.run_code(
            _test_program("/tmp/cr-agent-source.zip", commands, timeout_seconds),
            on_stdout=lambda data: streamed_stdout.append(data.line),
            on_stderr=lambda data: streamed_stderr.append(data.line),
            on_error=lambda _error: None,
            timeout=timeout_seconds + 60,
        )
        if execution.error is not None:
            raise RuntimeError("sandbox code interpreter returned an execution error")
        result = _read_remote_result(streamed_stdout)
        if result is None:
            result = _read_remote_result((execution.text or "").splitlines())
        if result is None:
            raise RuntimeError("sandbox did not return a structured test result")
        output_parts = []
        for item in result.get("commands", []):
            output_parts.append(
                "[{name}] exit_code={exit_code}\nstdout:\n{stdout}\nstderr:\n{stderr}".format(
                    name=item.get("name", "test"),
                    exit_code=item.get("exit_code", "unknown"),
                    stdout=_tail(item.get("stdout", "")),
                    stderr=_tail(item.get("stderr", "")),
                )
            )
        error = result.get("runner_error", "")
        timed_out = result.get("timed_out", False)
        if result.get("status") == "passed":
            message = "沙箱中的自动化测试全部通过。"
        elif result.get("status") == "failed":
            message = "沙箱中的自动化测试失败；请查看执行输出。"
        elif result.get("collection_failed"):
            message = "Python 测试在收集阶段中断，测试用例未运行；请检查沙箱模板中的依赖与导入配置。"
        elif timed_out:
            message = "自动化测试超过执行时限，结果未完成。"
        elif error:
            message = "沙箱测试未完成：" + error
        else:
            message = "沙箱未能完成自动化测试。"
        response = {
            "ran": bool(result.get("ran")),
            "status": result.get("status", "incomplete"),
            "message": message,
            "output": "\n".join(output_parts),
        }
    except Exception as error:
        response["output"] = "E2B SDK error: " + type(error).__name__
    finally:
        if sandbox is not None:
            try:
                sandbox.kill()
            except Exception:
                response["status"] = "incomplete"
                response["message"] = "测试结束但沙箱销毁失败，请检查腾讯云实例列表。"
                response["output"] = (response.get("output", "") + "\n沙箱销毁失败").strip()
    print(RESULT_PREFIX + json.dumps(response, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(
            RESULT_PREFIX
            + json.dumps(
                {
                    "ran": False,
                    "status": "incomplete",
                    "message": "E2B 启动器失败，请确认已安装 requirements-sandbox.txt。",
                    "output": "启动器错误类型: " + type(error).__name__,
                },
                ensure_ascii=False,
            )
        )
