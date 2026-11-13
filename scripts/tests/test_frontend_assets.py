"""Verify upgrade cache isolation without changing the application version."""
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]


class FrontendAssetsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="ugos-wol-assets-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        shutil.copytree(ROOT / "frontend", self.root / "frontend",
                        ignore=shutil.ignore_patterns("node_modules", ".DS_Store"))
        (self.root / "frontend/node_modules").symlink_to(ROOT / "frontend/node_modules", target_is_directory=True)

    def build(self):
        subprocess.run([os.environ.get("NODE_BIN", "node"), "build.mjs"],
                       cwd=self.root / "frontend", check=True, capture_output=True)
        web = self.root / "backend/web"
        html = (web / "index.html").read_text()
        urls = {f'{base}.{ext}': digest for base,digest,ext in re.findall(r'\./(icons|icon|style|app)\.([0-9a-f]{12})\.(svg|css|js)',html)}
        self.assertEqual(set(urls), {"icon.svg", "style.css", "app.js"})
        for name, digest in urls.items():
            data = (web / name).read_bytes()
            self.assertEqual(hashlib.sha256(data).hexdigest()[:12], digest)
            self.assertEqual(data, (self.root / "packaging/rootfs_common/www" / name).read_bytes())
            base,ext=name.rsplit(".",1)
            self.assertEqual(data,(web/f"{base}.{digest}.{ext}").read_bytes())
        urls['icons.svg'] = hashlib.sha256((web/'icons.svg').read_bytes()).hexdigest()[:12]
        self.assertEqual((web/'icons.svg').read_bytes(),(web/f"icons.{urls['icons.svg']}.svg").read_bytes())
        for file in web.iterdir():
            if file.suffix==".gz":
                self.assertEqual(gzip.decompress(file.read_bytes()),file.with_suffix("").read_bytes())
                self.assertEqual(file.read_bytes(),(self.root/"packaging/rootfs_common/www"/file.name).read_bytes())
        self.assertTrue((web/"app.js.gz").exists())
        self.assertTrue((web/"style.css.gz").exists())
        self.assertTrue((web/"index.html.gz").exists())
        self.assertNotIn("./icons.svg#", html)
        self.assertNotIn("./icons.svg#", (web / "app.js").read_text())
        self.assertNotIn("<use",html)
        data=(self.root/'frontend/icon-data.js').read_text().split('export default ',1)[1].strip().removesuffix(';')
        definitions=json.loads(data)
        for name in ['desktop-20','laptop-48','calendar_clock-20','os_windows-20']:
            self.assertIn(name,definitions)
            self.assertTrue(ET.fromstring('<svg>'+definitions[name]['body']+'</svg>').findall('.//path'))
        symbols = {node.attrib["id"] for node in ET.fromstring((web / "icons.svg").read_text())}
        references = re.findall(r'data-icon="([^"]+)"', html)
        self.assertTrue(references)
        self.assertTrue(set(references).issubset(symbols))
        self.assertIn("v0.5.0", html)
        return urls

    def test_stale_precompressed_assets_are_replaced(self):
        for destination in [self.root/"backend/web",self.root/"packaging/rootfs_common/www"]:
            destination.mkdir(parents=True)
            for file in ["app.js.gz","style.css.gz","index.html.gz"]:
                (destination/file).write_bytes(gzip.compress(b"obsolete frontend"))
        self.build()

    def test_same_inputs_keep_stable_urls(self):
        self.assertEqual(self.build(), self.build())

    def test_styles_change_without_a_version_bump(self):
        before = self.build()
        css = self.root / "frontend/style.css"
        css.write_text(css.read_text() + "\n/* cache regression fixture */\n")
        after = self.build()
        self.assertNotEqual(before["style.css"], after["style.css"])
        self.assertEqual(before["app.js"], after["app.js"])
        self.assertEqual(before["icons.svg"], after["icons.svg"])

    def test_icon_changes_update_embedded_paths_and_bundle(self):
        before = self.build()
        icon=self.root/'frontend/icons/desktop_20_regular.svg'
        icon.write_text(icon.read_text().replace('</svg>','<path d="M1 1h2v2H1z"/></svg>'))
        after = self.build()
        self.assertNotEqual(before["icons.svg"], after["icons.svg"])
        self.assertNotEqual(before["app.js"], after["app.js"])
        self.assertEqual(before["style.css"], after["style.css"])
        self.assertIn('d="M1 1h2v2H1z"',(self.root/'backend/web/index.html').read_text())


if __name__ == "__main__":
    unittest.main()
