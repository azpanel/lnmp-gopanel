#!/usr/bin/env python3
"""Exercise release modes, artifact integrity and publication without GitHub writes."""

import hashlib
from pathlib import Path
import runpy
import tempfile
import unittest
from unittest import mock

module = runpy.run_path(str(Path(__file__).with_name("release-mode.py")))
prepare, publish, verify_assets = module["prepare"], module["publish"], module["verify_assets"]
SHA = "a" * 40


def environment(**values):
    env = {"GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_REF_TYPE": "branch", "GITHUB_REF_NAME": "main",
           "GITHUB_SHA": SHA, "RELEASE_MODE": "fast", "RELEASE_TAG_INPUT": "v0.1.20"}
    env.update(values)
    return env


def artifacts(directory):
    directory = Path(directory)
    sums = []
    for arch in ("amd64", "arm64"):
        name = f"lnmp-gopanel_linux_{arch}.tar.gz"
        (directory / name).write_bytes(arch.encode())
        line = hashlib.sha256(arch.encode()).hexdigest() + "  " + name + "\n"
        (directory / (name + ".sha256")).write_text(line)
        sums.append(line)
    (directory / "SHA256SUMS").write_text("".join(sums))
    (directory / "install.sh").write_text("#!/bin/bash\n")


class ReleaseModes(unittest.TestCase):
    def test_branch_and_pr_events_cannot_enable_fast_publication(self):
        for event in ("push", "pull_request"):
            request = mock.Mock()
            result = prepare(environment(GITHUB_EVENT_NAME=event), request)
            self.assertEqual(result, {"mode": "full", "version": "dev-" + SHA[:12], "publish": "false", "new_tag": "false"})
            request.assert_not_called()

    def test_pushed_tags_and_manual_full_verification_keep_the_full_path(self):
        request = mock.Mock()
        for event in ("push", "workflow_dispatch"):
            result = prepare(environment(GITHUB_EVENT_NAME=event, GITHUB_REF_TYPE="tag", GITHUB_REF_NAME="v0.1.19",
                                         RELEASE_MODE="full", RELEASE_TAG_INPUT=""), request)
            self.assertEqual(result, {"mode": "full", "version": "v0.1.19", "publish": "true", "new_tag": "false"})
        result = prepare(environment(RELEASE_MODE="full", RELEASE_TAG_INPUT=""), request)
        self.assertEqual(result["publish"], "false")
        request.assert_not_called()

    def test_new_manual_versions_use_the_requested_version_in_both_modes(self):
        for mode in ("full", "fast"):
            request = mock.Mock(return_value=None)
            result = prepare(environment(RELEASE_MODE=mode), request)
            self.assertEqual(result, {"mode": mode, "version": "v0.1.20", "publish": "true", "new_tag": "true"})
            self.assertEqual(request.call_count, 2)

    def test_bad_input_is_rejected_before_network_access(self):
        for changes in ({"RELEASE_TAG_INPUT": ""}, {"RELEASE_MODE": "unknown"},
                        *({"RELEASE_TAG_INPUT": tag} for tag in ("v1.2", "1.2.3", "v01.2.3", "v1.2.3-rc", "v1.2.3\n", "v1.2.3; echo secret"))):
            request = mock.Mock()
            with self.subTest(changes=changes), self.assertRaises(RuntimeError):
                prepare(environment(**changes), request)
            request.assert_not_called()

    def test_existing_tags_releases_and_api_failures_are_not_ignored(self):
        for results in ([{"object": {}}], [None, {"draft": True}]):
            with self.assertRaises(RuntimeError):
                prepare(environment(), mock.Mock(side_effect=results))
        with self.assertRaisesRegex(RuntimeError, "offline"):
            prepare(environment(), mock.Mock(side_effect=RuntimeError("offline")))

    def test_fast_release_creates_tag_at_tested_sha_and_uploads_before_publishing(self):
        with tempfile.TemporaryDirectory() as directory:
            artifacts(directory)
            request, run = mock.Mock(return_value=None), mock.Mock()
            publish({"RELEASE_TAG": "v0.1.20", "RELEASE_SHA": SHA, "RELEASE_MODE": "fast", "NEW_TAG": "true"}, directory, request, run)
            request.assert_any_call("/git/refs", "POST", {"ref": "refs/tags/v0.1.20", "sha": SHA})
            created, published = [call.args[0] for call in run.call_args_list]
            self.assertIn("--draft", created)
            self.assertIn("--verify-tag", created)
            self.assertIn("--notes", created)
            self.assertEqual(created[created.index("--target") + 1], SHA)
            for name in module["ASSETS"]:
                self.assertIn(str(Path(directory) / name), created)
            self.assertEqual(published, ["gh", "release", "edit", "v0.1.20", "--draft=false", "--latest"])

    def test_interrupted_draft_can_resume_but_published_or_wrong_commit_cannot(self):
        env = {"RELEASE_TAG": "v0.1.20", "RELEASE_SHA": SHA, "RELEASE_MODE": "fast", "NEW_TAG": "true"}
        with tempfile.TemporaryDirectory() as directory:
            artifacts(directory)
            run = mock.Mock()
            request = mock.Mock(side_effect=[{"draft": True}, {"object": {"type": "commit", "sha": SHA}}])
            publish(env, directory, request, run)
            self.assertEqual(run.call_args_list[0].args[0][:3], ["gh", "release", "upload"])
            self.assertEqual(run.call_args_list[1].args[0][2], "edit")
            for responses in ([{"draft": False}], [None, {"object": {"type": "commit", "sha": "b" * 40}}]):
                run.reset_mock()
                with self.assertRaises(RuntimeError):
                    publish(env, directory, mock.Mock(side_effect=responses), run)
                run.assert_not_called()

    def test_corrupt_or_missing_assets_stop_before_creating_a_tag(self):
        env = {"RELEASE_TAG": "v0.1.20", "RELEASE_SHA": SHA, "RELEASE_MODE": "fast", "NEW_TAG": "true"}
        with tempfile.TemporaryDirectory() as directory:
            artifacts(directory)
            verify_assets(directory)
            for bad_file in ("lnmp-gopanel_linux_amd64.tar.gz", "lnmp-gopanel_linux_arm64.tar.gz.sha256", "SHA256SUMS", "install.sh"):
                artifacts(directory)
                (Path(directory) / bad_file).write_text("corrupt" if bad_file != "install.sh" else "")
                request, run = mock.Mock(), mock.Mock()
                with self.subTest(file=bad_file), self.assertRaises(RuntimeError):
                    publish(env, directory, request, run)
                request.assert_not_called()
                run.assert_not_called()

    def test_full_pushed_tag_retries_keep_existing_upload_behavior(self):
        with tempfile.TemporaryDirectory() as directory:
            artifacts(directory)
            for existing in (None, {"draft": False}):
                request, run = mock.Mock(return_value=existing), mock.Mock()
                publish({"RELEASE_TAG": "v0.1.19", "NEW_TAG": "false"}, directory, request, run)
                self.assertEqual(run.call_args.args[0][:3], ["gh", "release", "create" if existing is None else "upload"])
                self.assertEqual(request.call_count, 1)


if __name__ == "__main__":
    unittest.main()
