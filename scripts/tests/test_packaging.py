"""Offline packaging-flow checks; all payloads are dummy files in temp dirs."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]


class PackagingFlowTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="wol-package-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "scripts/lib").mkdir(parents=True)
        for name in ["pack-prebuilt.sh", "export-upk.sh", "lib/architectures.sh"]:
            shutil.copyfile(REPO / "scripts" / name, self.root / "scripts" / name)
        (self.root / "packaging").mkdir()
        (self.root / "packaging/project.yaml").write_text('app_id: com.test.wake\nversion: "9.9.9"\n')
        for arch in ["amd64", "arm64"]:
            binary = self.root / f"packaging/rootfs_{arch}/bin/wol"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"dummy binary; never executed")
        for name in ["icon.png", "www/index.html"]:
            asset = self.root / "packaging/rootfs_common" / name
            asset.parent.mkdir(parents=True, exist_ok=True)
            asset.write_text("test asset")
        toolbin = self.root / "tools"
        toolbin.mkdir()
        # Emulate the required packaging host only inside this isolated fixture.
        uname = toolbin / "uname"
        uname.write_text('#!/bin/sh\ncase "$1" in -s) echo Linux;; -m) echo x86_64;; esac\n')
        uname.chmod(0o755)
        self.tool = toolbin / "ugcli-stub"
        self.tool.write_text("#!" + sys.executable + '\n' + r'''
import json, os, pathlib, sys
with open(os.environ['WOL_TEST_LOG'], 'a') as log:
    log.write(json.dumps(sys.argv[1:]) + '\n')
if sys.argv[1] == 'pack':
    arch = sys.argv[sys.argv.index('--arch') + 1]
    build = int(sys.argv[sys.argv.index('--build') + 1])
    output = pathlib.Path('build_dir/pkgs/upk')
    output.mkdir(parents=True, exist_ok=True)
    (output / f'{arch}_nasync_com.test.wake_9.9.9.{build:04}.upk').write_text('dummy fixture, not an UPK')
''')
        self.tool.chmod(0o755)
        self.output = self.root / "out"
        self.env = dict(os.environ, PATH=str(toolbin) + os.pathsep + os.environ["PATH"],
                        UGCLI_TOOL=str(self.tool), UPK_OUTPUT_DIR=str(self.output),
                        BUILD_NUMBER="7", TARGET_ARCH="all", WOL_TEST_LOG=str(self.root / "calls.jsonl"))

    def run_script(self, name, **environment):
        return subprocess.run(["bash", f"scripts/{name}"], cwd=self.root,
                              env=dict(self.env, **environment), capture_output=True, text=True)

    def test_both_architectures_are_packed_and_exported(self):
        result = self.run_script("pack-prebuilt.sh")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(list(self.output.glob("*.upk"))), 2)
        self.assertFalse(list((self.root / "packaging/build_dir/pkgs/upk").glob("*.upk")))
        calls = [json.loads(line) for line in (self.root / "calls.jsonl").read_text().splitlines()]
        self.assertEqual([call[call.index("--arch") + 1] for call in calls if call[0] == "pack"], ["amd64", "arm64"])

    def test_single_target_is_not_confused_with_host_architecture(self):
        result = self.run_script("pack-prebuilt.sh", TARGET_ARCH="arm64")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([p.name for p in self.output.glob("*.upk")], ["arm64_nasync_com.test.wake_9.9.9.0007.upk"])

    def test_quoted_application_id_uses_the_same_export_name(self):
        (self.root / "packaging/project.yaml").write_text('app_id: "com.test.wake"\nversion: "9.9.9"\n')
        result = self.run_script("pack-prebuilt.sh", TARGET_ARCH="amd64")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([p.name for p in self.output.glob("*.upk")], ["amd64_nasync_com.test.wake_9.9.9.0007.upk"])

    def test_existing_output_is_never_overwritten_or_partly_exported(self):
        self.output.mkdir()
        existing = self.output / "arm64_nasync_com.test.wake_9.9.9.0007.upk"
        existing.write_text("keep existing artifact")
        result = self.run_script("pack-prebuilt.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(existing.read_text(), "keep existing artifact")
        self.assertEqual(len(list(self.output.glob("*.upk"))), 1)
        self.assertEqual(len(list((self.root / "packaging/build_dir/pkgs/upk").glob("*.upk"))), 2)

    def test_missing_second_source_does_not_move_first_source(self):
        source = self.root / "packaging/build_dir/pkgs/upk/amd64_nasync_com.test.wake_9.9.9.0007.upk"
        source.parent.mkdir(parents=True)
        source.write_text("dummy fixture")
        result = self.run_script("export-upk.sh")
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(source.exists())
        self.assertFalse(self.output.exists())

    def test_unknown_architecture_is_rejected_before_packaging(self):
        result = self.run_script("pack-prebuilt.sh", TARGET_ARCH="other")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.root / "calls.jsonl").exists())


if __name__ == "__main__":
    unittest.main()
