from firebase_sub.action_track import ActionMan, ActionTrack, ActionType


def test_empty_check():
    # Given an empty action dict
    ad = ActionTrack({})
    # When we check for the action
    # Then it hadn't happened, and we need to action
    assert ad.to_action(ActionType.TOOT, "some_value")


def test_set_check():
    # Given an empty action dict
    ad = ActionTrack({})
    # That we set an action on
    ad.action(ActionType.TOOT, "some_value")
    # When we check for the action
    # Then it happened, and we don't need to action
    assert not ad.to_action(ActionType.TOOT, "some_value")


def test_permiance_check():
    # Given an empty action dict
    ad = ActionTrack({})
    # That we set an action on
    ad.action(ActionType.TOOT, "some_value")
    tmp = dict(ad)
    new_me = ActionTrack(tmp)
    # When we check for the action
    # Then it happened
    assert not new_me.to_action(ActionType.TOOT, "some_value")


def test_action_runner():
    am = ActionMan()
    run_count = 0

    def my_callback(**kwargs):
        nonlocal run_count
        run_count += 1

    am.bind(ActionType.TOOT, my_callback)
    ad, actioned = am.run(action_dict={}, action_key="some_value")
    assert run_count == 1
    assert actioned
    ad, actioned = am.run(action_dict=ad, action_key="some_value")
    assert run_count == 1
    assert not actioned

    assert "toot" in ad
