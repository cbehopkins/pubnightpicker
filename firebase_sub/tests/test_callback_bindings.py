import ast
from pathlib import Path


def _find_function(module_ast: ast.Module, name: str) -> ast.FunctionDef:
    for node in module_ast.body:
        if isinstance(node, ast.FunctionDef) and node.name == name:
            return node
    raise AssertionError(f"Function {name!r} not found")


def _bind_target(call: ast.Call) -> tuple[str, str] | None:
    action_arg = call.args[0]
    callback_arg = call.args[1]
    if not isinstance(action_arg, ast.Attribute):
        return None
    if isinstance(callback_arg, ast.Name):
        return (action_arg.attr, callback_arg.id)
    return None


def test_poll_complete_actions_binds_email_and_pemail_paths():
    sub_events_path = (
        Path(__file__).resolve().parent.parent
        / "firebase_sub"
        / "cli"
        / "sub_events.py"
    )
    source = sub_events_path.read_text(encoding="utf-8")
    tree = ast.parse(source)
    fn = _find_function(tree, "poll_complete_actions")

    bind_calls = [
        node
        for node in ast.walk(fn)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Attribute)
        and node.func.attr == "bind"
    ]

    assert len(bind_calls) == 3

    action_targets = []
    for call in bind_calls:
        bind_target = _bind_target(call)
        if bind_target is not None:
            action_targets.append(bind_target)

    assert ("EMAIL", "send_mail_list_email") in action_targets
    assert ("PEMAIL", "send_personal_email") in action_targets
    assert ("PUSH", "send_push_i") in action_targets


def test_poll_open_actions_binds_email_and_push_paths():
    sub_events_path = (
        Path(__file__).resolve().parent.parent
        / "firebase_sub"
        / "cli"
        / "sub_events.py"
    )
    source = sub_events_path.read_text(encoding="utf-8")
    tree = ast.parse(source)
    fn = _find_function(tree, "poll_open_actions")

    bind_calls = [
        node
        for node in ast.walk(fn)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Attribute)
        and node.func.attr == "bind"
    ]

    assert len(bind_calls) == 2

    action_targets = []
    for call in bind_calls:
        bind_target = _bind_target(call)
        if bind_target is not None:
            action_targets.append(bind_target)

    assert ("EMAIL", "send_poll_open_email_i") in action_targets
    assert ("PUSH", "send_poll_open_push_i") in action_targets


def test_cli_exposes_independent_dummy_push_flag():
    sub_events_path = (
        Path(__file__).resolve().parent.parent
        / "firebase_sub"
        / "cli"
        / "sub_events.py"
    )
    source = sub_events_path.read_text(encoding="utf-8")
    tree = ast.parse(source)

    click_options = [
        decorator
        for node in tree.body
        if isinstance(node, ast.FunctionDef) and node.name == "cli"
        for decorator in node.decorator_list
        if isinstance(decorator, ast.Call)
        and isinstance(decorator.func, ast.Attribute)
        and decorator.func.attr == "option"
    ]

    option_values = []
    for decorator in click_options:
        if decorator.args and isinstance(decorator.args[0], ast.Constant):
            option_values.append(str(decorator.args[0].value))

    assert "--dummy-push/--no-dummy-push" in option_values
