# Third-party components

The frontend bundles `@ugreen-nas/core` 1.7.42, published by 深圳市绿联科技股份有限公司 under the MIT license. Source distribution and package metadata: https://www.npmjs.com/package/@ugreen-nas/core . The MIT permission and warranty terms are reproduced in the adjacent LICENSE file (with each component retaining its own authorship).

The executable includes the Go runtime and standard library. Their BSD-style license is reproduced in `GO-LICENSE.txt`; source is available at https://go.dev/src/ .

esbuild 0.28.2 (MIT) is a build-time dependency and is not required on the NAS. Its dependency metadata and integrity hashes are recorded in `frontend/package-lock.json`.

The official UGREEN `ugcli` is used only for packaging; it is not redistributed in the source archive or installed application. Playwright and Chrome were used for development checks and are not included in the application.

Microsoft Fluent System Icons 1.1.345 (MIT) provides the interface SVG icons. The app icon adapts the Fluent power symbol; its background circles are app artwork. Source: https://github.com/microsoft/fluentui-system-icons . License: FLUENT-ICONS-LICENSE.txt. Selected original SVG assets are vendored under frontend/icons; runtime SVG geometry is generated locally and embedded directly, with no external icon service or sprite request.

Font Awesome Free 6.7.2 provides the Windows, Apple and Linux brand SVG icons (CC BY 4.0). Copyright Fonticons, Inc. Source: https://github.com/FortAwesome/Font-Awesome/tree/6.7.2/svgs/brands . Icons are scaled and recolored to match the interface; original path geometry is retained. Full license: FONT-AWESOME-LICENSE.txt. System selection is user-provided and does not imply affiliation or endorsement.
