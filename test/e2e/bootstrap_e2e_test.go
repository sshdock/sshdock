//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapInstallsBinariesDataDirsAndService(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()

	sourceBinDir := filepath.Join(tmp, "source-bin")
	if err := os.MkdirAll(sourceBinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source bin: %v", err)
	}
	rhumbasePath := filepath.Join(sourceBinDir, "rhumbase")
	rhumbasedPath := filepath.Join(sourceBinDir, "rhumbased")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasePath, "./cmd/rhumbase")
	runCommand(t, root, nil, "go", "build", "-o", rhumbasedPath, "./cmd/rhumbased")

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeFakeCommand(t, fakeBinDir, "docker", `#!/bin/sh
printf 'docker %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "caddy", `#!/bin/sh
printf 'caddy %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "sudo", `#!/bin/sh
printf 'sudo %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "id", `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "-u" ]; then
	echo 0
	exit 0
fi
printf 'id %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 1
`)
	writeFakeCommand(t, fakeBinDir, "useradd", `#!/bin/sh
printf 'useradd %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "usermod", `#!/bin/sh
printf 'usermod %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "visudo", `#!/bin/sh
printf 'visudo %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)

	installRoot := filepath.Join(tmp, "root")
	env := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_TAG=test-local",
		"RHUMBASE_BOOTSTRAP_ROOT="+installRoot,
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"RHUMBASE_BOOTSTRAP_INSTALL_DEPS=0",
		"RHUMBASE_BOOTSTRAP_SKIP_CHOWN=1",
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
	)

	runCommand(t, root, env, "bash", "scripts/bootstrap.sh")

	assertExecutable(t, filepath.Join(installRoot, "usr/local/bin/rhumbase"))
	assertExecutable(t, filepath.Join(installRoot, "usr/local/bin/rhumbased"))
	assertExecutable(t, filepath.Join(installRoot, "usr/local/bin/rhumbase-git-receive"))
	assertExecutable(t, filepath.Join(installRoot, "usr/local/bin/rhumbase-dashboard"))
	assertDir(t, filepath.Join(installRoot, "var/lib/rhumbase"))
	assertDir(t, filepath.Join(installRoot, "var/lib/rhumbase/apps"))
	assertDir(t, filepath.Join(installRoot, "var/lib/rhumbase/dashboard"))

	wrapper := readFile(t, filepath.Join(installRoot, "usr/local/bin/rhumbase-git-receive"))
	for _, want := range []string{
		"export RHUMBASE_DATA_DIR=/var/lib/rhumbase",
		"export RHUMBASE_APPS_DIR=/var/lib/rhumbase/apps",
		"export RHUMBASE_COMPOSE_RUNNER=docker",
		"exec /usr/local/bin/rhumbased git-receive",
	} {
		if !strings.Contains(wrapper, want) {
			t.Fatalf("git receive wrapper missing %q:\n%s", want, wrapper)
		}
	}

	dashboardWrapper := readFile(t, filepath.Join(installRoot, "usr/local/bin/rhumbase-dashboard"))
	for _, want := range []string{
		"export RHUMBASE_DATA_DIR=/var/lib/rhumbase",
		"export RHUMBASE_COMPOSE_RUNNER=docker",
		"exec /usr/local/bin/rhumbased dashboard",
	} {
		if !strings.Contains(dashboardWrapper, want) {
			t.Fatalf("dashboard wrapper missing %q:\n%s", want, dashboardWrapper)
		}
	}

	gitSudoersPath := filepath.Join(installRoot, "etc/sudoers.d/rhumbase-git-receive")
	gitSudoers := readFile(t, gitSudoersPath)
	for _, want := range []string{
		`Defaults:git env_keep += "SSH_ORIGINAL_COMMAND"`,
		"git ALL=(rhumbase) NOPASSWD: /usr/local/bin/rhumbase-git-receive",
	} {
		if !strings.Contains(gitSudoers, want) {
			t.Fatalf("git sudoers missing %q:\n%s", want, gitSudoers)
		}
	}
	assertFileMode(t, gitSudoersPath, 0o440)

	dashboardSudoersPath := filepath.Join(installRoot, "etc/sudoers.d/rhumbase-dashboard")
	dashboardSudoers := readFile(t, dashboardSudoersPath)
	for _, want := range []string{
		"dashboard ALL=(rhumbase) NOPASSWD: /usr/local/bin/rhumbase-dashboard",
	} {
		if !strings.Contains(dashboardSudoers, want) {
			t.Fatalf("dashboard sudoers missing %q:\n%s", want, dashboardSudoers)
		}
	}
	assertFileMode(t, dashboardSudoersPath, 0o440)

	unitPath := filepath.Join(installRoot, "etc/systemd/system/rhumbased.service")
	unit := readFile(t, unitPath)
	for _, want := range []string{
		"After=network-online.target docker.service",
		"Requires=docker.service",
		"User=rhumbase",
		"Group=rhumbase",
		"Environment=RHUMBASE_DATA_DIR=/var/lib/rhumbase",
		"Environment=RHUMBASE_GIT_HOST=server",
		"Environment=RHUMBASE_COMPOSE_RUNNER=docker",
		"Environment=RHUMBASE_CADDY_CONFIG_PATH=/etc/caddy/rhumbase.caddyfile",
		"ExecStart=/usr/local/bin/rhumbased daemon",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("service unit missing %q:\n%s", want, unit)
		}
	}
	for _, notWant := range []string{
		"Environment=RHUMBASE_SSH_LISTEN_ADDR=:2222",
		"ExecStart=/usr/local/bin/rhumbased\n",
	} {
		if strings.Contains(unit, notWant) {
			t.Fatalf("service unit should not contain %q:\n%s", notWant, unit)
		}
	}

	fakeLog := readFile(t, fakeLogPath)
	for _, want := range []string{
		"docker version",
		"docker compose version",
		"caddy version",
		"systemctl --version",
		"sudo -V",
		"useradd --system --home /var/lib/rhumbase --shell /usr/sbin/nologin rhumbase",
		"useradd --system --home /var/lib/rhumbase/git --shell /bin/sh git",
		"useradd --system --home /var/lib/rhumbase/dashboard --shell /bin/sh dashboard",
		"usermod --shell /bin/sh git",
		"usermod --home /var/lib/rhumbase/dashboard --shell /bin/sh dashboard",
		"visudo -cf ",
		"systemctl daemon-reload",
		"systemctl enable rhumbased.service",
		"systemctl restart rhumbased.service",
	} {
		if !strings.Contains(fakeLog, want) {
			t.Fatalf("fake command log missing %q:\n%s", want, fakeLog)
		}
	}
}

func TestBootstrapRequiresTag(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	env := append(os.Environ(),
		"RHUMBASE_BOOTSTRAP_ROOT="+filepath.Join(tmp, "root"),
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+tmp,
		"RHUMBASE_BOOTSTRAP_INSTALL_DEPS=0",
		"RHUMBASE_BOOTSTRAP_SKIP_USER=1",
		"RHUMBASE_BOOTSTRAP_SKIP_CHOWN=1",
	)

	cmd := exec.Command("bash", "scripts/bootstrap.sh")
	cmd.Dir = root
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("bootstrap succeeded without RHUMBASE_TAG:\n%s", output)
	}
	if !strings.Contains(string(output), "RHUMBASE_TAG is required") {
		t.Fatalf("bootstrap missing tag output = %s", output)
	}
}

func TestBootstrapInstallsDependenciesAndConfiguresHost(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sourceBinDir := buildBootstrapSourceBinaries(t, root, tmp)

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeDependencyInstallFakeCommands(t, fakeBinDir)

	installRoot := filepath.Join(tmp, "root")
	env := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_TAG=test-local",
		"RHUMBASE_BOOTSTRAP_ROOT="+installRoot,
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"RHUMBASE_BOOTSTRAP_INSTALL_DEPS=1",
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
		"RHUMBASE_BOOTSTRAP_TEST_OS_RELEASE=ID=ubuntu\nVERSION_CODENAME=noble\nUBUNTU_CODENAME=noble\n",
	)

	runCommand(t, root, env, "bash", "scripts/bootstrap.sh")

	fakeLog := readFile(t, fakeLogPath)
	for _, want := range []string{
		"apt-get update",
		"apt-get install -y ca-certificates curl gnupg git openssh-server sudo debian-keyring debian-archive-keyring apt-transport-https",
		"curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o " + filepath.Join(installRoot, "etc/apt/keyrings/docker.asc"),
		"chmod a+r " + filepath.Join(installRoot, "etc/apt/keyrings/docker.asc"),
		"apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key -o " + filepath.Join(installRoot, "tmp/rhumbase-caddy-stable.gpg.key"),
		"gpg --batch --yes --dearmor -o " + filepath.Join(installRoot, "usr/share/keyrings/caddy-stable-archive-keyring.gpg") + " " + filepath.Join(installRoot, "tmp/rhumbase-caddy-stable.gpg.key"),
		"curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt -o " + filepath.Join(installRoot, "etc/apt/sources.list.d/caddy-stable.list"),
		"apt-get install -y caddy",
		"systemctl enable --now docker",
		"systemctl enable --now ssh",
		"systemctl enable --now caddy",
		"usermod -aG docker rhumbase",
		"usermod --shell /bin/sh git",
		"usermod --home /var/lib/rhumbase/dashboard --shell /bin/sh dashboard",
		"visudo -cf ",
		"chown -R rhumbase:rhumbase " + filepath.Join(installRoot, "var/lib/rhumbase"),
		"chown -R git:git " + filepath.Join(installRoot, "var/lib/rhumbase/git"),
		"chown dashboard:dashboard " + filepath.Join(installRoot, "var/lib/rhumbase/dashboard") + " " + filepath.Join(installRoot, "var/lib/rhumbase/dashboard/.ssh") + " " + filepath.Join(installRoot, "var/lib/rhumbase/dashboard/.ssh/authorized_keys"),
		"chmod 0755 " + filepath.Join(installRoot, "var/lib/rhumbase/git"),
		"chmod 0700 " + filepath.Join(installRoot, "var/lib/rhumbase/git/.ssh"),
		"touch " + filepath.Join(installRoot, "var/lib/rhumbase/git/.ssh/authorized_keys"),
		"chmod 0600 " + filepath.Join(installRoot, "var/lib/rhumbase/git/.ssh/authorized_keys"),
	} {
		if !strings.Contains(fakeLog, want) {
			t.Fatalf("fake command log missing %q:\n%s", want, fakeLog)
		}
	}

	dockerSource := readFile(t, filepath.Join(installRoot, "etc/apt/sources.list.d/docker.sources"))
	for _, want := range []string{
		"URIs: https://download.docker.com/linux/ubuntu",
		"Suites: noble",
		"Architectures: amd64",
		"Signed-By: /etc/apt/keyrings/docker.asc",
	} {
		if !strings.Contains(dockerSource, want) {
			t.Fatalf("docker source missing %q:\n%s", want, dockerSource)
		}
	}

	caddySource := readFile(t, filepath.Join(installRoot, "etc/apt/sources.list.d/caddy-stable.list"))
	for _, want := range []string{
		"dl.cloudsmith.io/public/caddy/stable",
		"caddy-stable-archive-keyring.gpg",
	} {
		if !strings.Contains(caddySource, want) {
			t.Fatalf("caddy source missing %q:\n%s", want, caddySource)
		}
	}

	caddyfilePath := filepath.Join(installRoot, "etc/caddy/Caddyfile")
	caddyfile := readFile(t, caddyfilePath)
	if count := strings.Count(caddyfile, "import /etc/caddy/rhumbase.caddyfile"); count != 1 {
		t.Fatalf("Caddyfile import count = %d:\n%s", count, caddyfile)
	}
	assertFileMode(t, filepath.Join(installRoot, "etc/caddy/rhumbase.caddyfile"), 0o644)

	runCommand(t, root, env, "bash", "scripts/bootstrap.sh")
	caddyfile = readFile(t, caddyfilePath)
	if count := strings.Count(caddyfile, "import /etc/caddy/rhumbase.caddyfile"); count != 1 {
		t.Fatalf("rerun Caddyfile import count = %d:\n%s", count, caddyfile)
	}
}

func TestBootstrapSkipsDependencyInstallWhenRuntimeAlreadyWorks(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sourceBinDir := buildBootstrapSourceBinaries(t, root, tmp)

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeWorkingRuntimeFakeCommands(t, fakeBinDir)

	env := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_TAG=test-local",
		"RHUMBASE_BOOTSTRAP_ROOT="+filepath.Join(tmp, "root"),
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"RHUMBASE_BOOTSTRAP_INSTALL_DEPS=1",
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
		"RHUMBASE_BOOTSTRAP_TEST_OS_RELEASE=ID=debian\nVERSION_CODENAME=bookworm\n",
	)

	runCommand(t, root, env, "bash", "scripts/bootstrap.sh")

	fakeLog := readFile(t, fakeLogPath)
	if strings.Contains(fakeLog, "apt-get install -y docker-ce") {
		t.Fatalf("bootstrap installed Docker even though runtime worked:\n%s", fakeLog)
	}
	if strings.Contains(fakeLog, "apt-get install -y caddy") {
		t.Fatalf("bootstrap installed Caddy even though runtime worked:\n%s", fakeLog)
	}
	for _, want := range []string{
		"apt-get install -y ca-certificates curl gnupg git openssh-server sudo debian-keyring debian-archive-keyring apt-transport-https",
		"docker version",
		"docker compose version",
		"caddy version",
	} {
		if !strings.Contains(fakeLog, want) {
			t.Fatalf("fake command log missing %q:\n%s", want, fakeLog)
		}
	}
}

func TestBootstrapRetriesAptLocks(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sourceBinDir := buildBootstrapSourceBinaries(t, root, tmp)

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeDependencyInstallFakeCommands(t, fakeBinDir)
	writeFakeCommand(t, fakeBinDir, "apt-get", `#!/bin/sh
printf 'apt-get %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
lock_marker="$(dirname "$0")/apt-lock-seen"
if [ ! -f "$lock_marker" ]; then
	touch "$lock_marker"
	printf 'E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 123 (unattended-upgr)\n' >&2
	printf 'E: Unable to acquire the dpkg frontend lock (/var/lib/dpkg/lock-frontend), is another process using it?\n' >&2
	exit 100
fi
case " $* " in
	*" docker-ce "*)
		touch "$(dirname "$0")/docker-installed"
		;;
	*" caddy "*)
		touch "$(dirname "$0")/caddy-installed"
		;;
esac
exit 0
`)

	env := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_TAG=test-local",
		"RHUMBASE_BOOTSTRAP_ROOT="+filepath.Join(tmp, "root"),
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"RHUMBASE_BOOTSTRAP_INSTALL_DEPS=1",
		"RHUMBASE_BOOTSTRAP_APT_LOCK_WAIT_SECONDS=0",
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
		"RHUMBASE_BOOTSTRAP_TEST_OS_RELEASE=ID=ubuntu\nVERSION_CODENAME=noble\nUBUNTU_CODENAME=noble\n",
	)

	runCommand(t, root, env, "bash", "scripts/bootstrap.sh")

	fakeLog := readFile(t, fakeLogPath)
	if count := strings.Count(fakeLog, "apt-get update"); count < 2 {
		t.Fatalf("apt-get update attempts = %d, want retry:\n%s", count, fakeLog)
	}
}

func TestBootstrapRejectsUnsupportedInstallOS(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sourceBinDir := buildBootstrapSourceBinaries(t, root, tmp)

	fakeBinDir := filepath.Join(tmp, "fake-bin")
	fakeLogPath := filepath.Join(tmp, "fake-commands.log")
	writeDependencyInstallFakeCommands(t, fakeBinDir)

	env := append(os.Environ(),
		"PATH="+fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"RHUMBASE_TAG=test-local",
		"RHUMBASE_BOOTSTRAP_ROOT="+filepath.Join(tmp, "root"),
		"RHUMBASE_BOOTSTRAP_SOURCE_BIN_DIR="+sourceBinDir,
		"RHUMBASE_BOOTSTRAP_INSTALL_DEPS=1",
		"RHUMBASE_BOOTSTRAP_FAKE_LOG="+fakeLogPath,
		"RHUMBASE_BOOTSTRAP_TEST_OS_RELEASE=ID=fedora\nVERSION_CODENAME=\n",
	)

	cmd := exec.Command("bash", "scripts/bootstrap.sh")
	cmd.Dir = root
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("bootstrap succeeded on unsupported OS:\n%s", output)
	}
	if !strings.Contains(string(output), "unsupported OS fedora") {
		t.Fatalf("bootstrap unsupported OS output = %s", output)
	}
}

func TestBootstrapUsesAtomicBinaryReplacement(t *testing.T) {
	script := readFile(t, filepath.Join("..", "..", "scripts/bootstrap.sh"))
	if !strings.Contains(script, `mv -f "$tmp_bin" "$target"`) {
		t.Fatalf("bootstrap should replace binaries with atomic mv to avoid ETXTBSY:\n%s", script)
	}
	if strings.Contains(script, `cp "$source/$bin" "$bin_dir_actual/$bin"`) {
		t.Fatalf("bootstrap still copies directly over installed binaries:\n%s", script)
	}
}

func TestReleaseWorkflowBuildsExpectedArtifacts(t *testing.T) {
	workflow := readFile(t, filepath.Join("..", "..", ".github/workflows/release.yml"))
	for _, want := range []string{
		"on:",
		"tags:",
		"'v*'",
		"GOOS: linux",
		"- amd64",
		"- arm64",
		"GOARCH: ${{ matrix.arch }}",
		"rhumbase_${{ github.ref_name }}_linux_${{ matrix.arch }}.tar.gz",
		"softprops/action-gh-release@v2",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q:\n%s", want, workflow)
		}
	}
}

func writeFakeCommand(t *testing.T, dir string, name string, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll fake bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile fake command %s: %v", name, err)
	}
}

func buildBootstrapSourceBinaries(t *testing.T, root string, tmp string) string {
	t.Helper()
	sourceBinDir := filepath.Join(tmp, "source-bin")
	if err := os.MkdirAll(sourceBinDir, 0o755); err != nil {
		t.Fatalf("MkdirAll source bin: %v", err)
	}
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "rhumbase"), "./cmd/rhumbase")
	runCommand(t, root, nil, "go", "build", "-o", filepath.Join(sourceBinDir, "rhumbased"), "./cmd/rhumbased")
	return sourceBinDir
}

func writeDependencyInstallFakeCommands(t *testing.T, fakeBinDir string) {
	t.Helper()
	writeFakeCommand(t, fakeBinDir, "apt-get", `#!/bin/sh
printf 'apt-get %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
case " $* " in
	*" docker-ce "*)
		touch "$(dirname "$0")/docker-installed"
		;;
	*" caddy "*)
		touch "$(dirname "$0")/caddy-installed"
		;;
esac
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "curl", `#!/bin/sh
printf 'curl %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
	while [ "$#" -gt 0 ]; do
		if [ "$1" = "-o" ]; then
			mkdir -p "$(dirname "$2")"
			case "$2" in
				*caddy-stable.list)
					printf 'deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main\n' > "$2"
					;;
				*)
					printf 'fake key\n' > "$2"
					;;
			esac
			exit 0
		fi
	shift
done
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "dpkg", `#!/bin/sh
printf 'dpkg %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
if [ "$1" = "--print-architecture" ]; then
	echo amd64
	exit 0
fi
if [ "$1" = "-s" ]; then
	exit 1
fi
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "gpg", `#!/bin/sh
printf 'gpg %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		mkdir -p "$(dirname "$2")"
		printf 'fake keyring\n' > "$2"
		exit 0
	fi
	shift
done
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "docker", `#!/bin/sh
printf 'docker %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
if [ -f "$(dirname "$0")/docker-installed" ]; then
	exit 0
fi
exit 1
`)
	writeFakeCommand(t, fakeBinDir, "caddy", `#!/bin/sh
printf 'caddy %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
if [ -f "$(dirname "$0")/caddy-installed" ]; then
	exit 0
fi
exit 1
`)
	writeFakeCommand(t, fakeBinDir, "git", `#!/bin/sh
printf 'git %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "ssh", `#!/bin/sh
printf 'ssh %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "sshd", `#!/bin/sh
printf 'sshd %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "sudo", `#!/bin/sh
printf 'sudo %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "systemctl", `#!/bin/sh
printf 'systemctl %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "id", `#!/bin/sh
if [ "$#" -eq 1 ] && [ "$1" = "-u" ]; then
	echo 0
	exit 0
fi
printf 'id %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 1
`)
	writeFakeCommand(t, fakeBinDir, "useradd", `#!/bin/sh
printf 'useradd %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "usermod", `#!/bin/sh
printf 'usermod %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "visudo", `#!/bin/sh
printf 'visudo %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "chown", `#!/bin/sh
printf 'chown %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "chmod", `#!/bin/sh
printf 'chmod %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
/bin/chmod "$@"
`)
	writeFakeCommand(t, fakeBinDir, "touch", `#!/bin/sh
printf 'touch %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
/usr/bin/touch "$@"
`)
}

func writeWorkingRuntimeFakeCommands(t *testing.T, fakeBinDir string) {
	t.Helper()
	writeDependencyInstallFakeCommands(t, fakeBinDir)
	writeFakeCommand(t, fakeBinDir, "docker", `#!/bin/sh
printf 'docker %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
	writeFakeCommand(t, fakeBinDir, "caddy", `#!/bin/sh
printf 'caddy %s\n' "$*" >> "$RHUMBASE_BOOTSTRAP_FAKE_LOG"
exit 0
`)
}

func assertExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat executable %s: %v", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable: %v", path, info.Mode().Perm())
	}
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat dir %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(data)
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %v, want %v", path, got, want)
	}
}
