import ast
from pathlib import Path


def _find_function(module_ast: ast.Module, name: str) -> ast.FunctionDef:
    for node in module_ast.body:
        if isinstance(node, ast.FunctionDef) and node.name == name:
            return node
    raise AssertionError(f"Function {name!r} not found")


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

    assert len(bind_calls) == 2

    action_targets = []
    for call in bind_calls:
        action_arg = call.args[0]
        callback_arg = call.args[1]
        if isinstance(action_arg, ast.Attribute) and isinstance(callback_arg, ast.Name):
            action_targets.append((action_arg.attr, callback_arg.id))

    assert ("EMAIL", "send_mail_list_email") in action_targets
    assert ("PEMAIL", "send_personal_email") in action_targets
