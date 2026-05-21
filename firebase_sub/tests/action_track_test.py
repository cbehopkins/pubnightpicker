from firebase_sub.action_track import ActionMan, ActionTrack, ActionType


def test_empty_check():
    # Given an empty action dict
    ad = ActionTrack({})
    # When we check for the action
    # Then it hadn't happened, and we need to action
    assert ad.to_action(ActionType.EMAIL, "some_value")


def test_set_check():
    # Given an empty action dict
    ad = ActionTrack({})
    # That we set an action on
    ad.action(ActionType.EMAIL, "some_value")
    # When we check for the action
    # Then it happened, and we don't need to action
    assert not ad.to_action(ActionType.EMAIL, "some_value")


def test_permiance_check():
    # Given an empty action dict
    ad = ActionTrack({})
    # That we set an action on
    ad.action(ActionType.EMAIL, "some_value")
    tmp = dict(ad)
    new_me = ActionTrack(tmp)
    # When we check for the action
    # Then it happened
    assert not new_me.to_action(ActionType.EMAIL, "some_value")


def test_action_runner():
    am = ActionMan()
    run_count = 0

    def my_callback(**kwargs):
        nonlocal run_count
        run_count += 1

    am.bind(ActionType.EMAIL, my_callback)
    ad, actioned = am.run(action_dict={}, action_key="some_value")
    assert run_count == 1
    assert actioned
    ad, actioned = am.run(action_dict=ad, action_key="some_value")
    assert run_count == 1
    assert not actioned

    assert "email" in ad


def test_action_runner_supports_per_action_dummy_override():
    am = ActionMan(dummy_run=True)
    observed: list[tuple[ActionType, bool]] = []

    def email_callback(*, dummy_run: bool, **kwargs):
        _ = kwargs
        observed.append((ActionType.EMAIL, dummy_run))

    def push_callback(*, dummy_run: bool, **kwargs):
        _ = kwargs
        observed.append((ActionType.PUSH, dummy_run))

    am.bind(ActionType.EMAIL, email_callback)
    am.bind(ActionType.PUSH, push_callback, dummy_run=False)

    am.run(action_dict={}, action_key="some_value")

    assert (ActionType.EMAIL, True) in observed
    assert (ActionType.PUSH, False) in observed


def test_filter_returns_true_when_any_bound_action_pending():
    am = ActionMan()
    am.bind(ActionType.EMAIL, lambda **kwargs: None)
    am.bind(ActionType.PUSH, lambda **kwargs: None)

    assert am.filter(action_dict={}, action_key="poll-1")


def test_filter_returns_false_when_all_bound_actions_done():
    am = ActionMan()
    am.bind(ActionType.EMAIL, lambda **kwargs: None)
    am.bind(ActionType.PUSH, lambda **kwargs: None)
    action_dict = {
        "email": ["poll-1"],
        "push": ["poll-1"],
    }

    assert not am.filter(action_dict=action_dict, action_key="poll-1")


def test_mark_done_marks_all_bound_actions_for_key():
    am = ActionMan()
    am.bind(ActionType.EMAIL, lambda **kwargs: None)
    am.bind(ActionType.PUSH, lambda **kwargs: None)

    action_dict = am.mark_done(action_dict={}, action_key="poll-1")

    assert "poll-1" in action_dict["email"]
    assert "poll-1" in action_dict["push"]
