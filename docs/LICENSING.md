# Licensing and provenance

SSHDock source is available under the [Apache License, Version 2.0](../LICENSE), SPDX identifier `Apache-2.0`, except for third-party material identified below. The root license is the unchanged [official Apache-2.0 text](https://www.apache.org/licenses/LICENSE-2.0.txt); its appendix is an application template, not an assertion of ownership over third-party work.

## Third-party material

- The bundled [Bubble Tea skill](../.agents/skills/bubbletea/SKILL.md) retains MIT licensing. Its five original files match [GGPrompts/TFE revision `cc1ccd95307b73309eed233daeaac2aed54ab5e1`](https://github.com/GGPrompts/TFE/tree/cc1ccd95307b73309eed233daeaac2aed54ab5e1/.claude/skills/bubbletea). The original [MIT license and copyright notice](../.agents/skills/bubbletea/LICENSE) are preserved alongside them.
- Go dependencies and the Go runtime retain their own licenses. Release archives include `LICENSE` and `THIRD_PARTY_NOTICES`, collected from the toolchain and modules used to build the production commands for the target platform. To reproduce the notices, run `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 bash scripts/release-notices.sh /tmp/sshdock-notices.txt` with the release toolchain and dependencies.
- Framework examples download or generate official starters during image builds; software recipes reference upstream images. Those applications, images, and their dependencies retain upstream licensing. SSHDock's license does not relicense them.
- Manual volume operations download a pinned [official Debian image](https://hub.docker.com/_/debian) for GNU tar. That image and its packages retain upstream licenses and are not bundled in SSHDock release archives.

## Contributor provenance

The initial review for [issue #53](https://github.com/sshdock/sshdock/issues/53) checked Git authorship, copied source, example provenance, and binary packaging. The existing history contains two author spellings with the same email address and no separate third-party source contributions beyond the skill above. Git attribution records authorship; it is not an assignment of copyright or independent proof of ownership. No new copyright holder is asserted by adding the license.

Contributors must have the right to submit their work under Apache-2.0 and preserve third-party notices. Flag any unresolved ownership or incompatible licensing before including the affected material in a release. Do not assume that an upstream dependency or copied file becomes Apache-2.0 merely by being used here.
