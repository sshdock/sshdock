window.SSHDOCK_DOCS = [
  {
    id: 'quick-start',
    title: 'Quick start',
    group: 'Get started',
    description: 'Go from one VPS to your first Compose app.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/README.md#quick-start',
    body: `
      <p>SSHDock runs your Docker Compose apps on one Linux VPS. Install it once, push your app over Git, and operate it over SSH. This guide uses the published <strong>v1.1.1</strong> release.</p>
      <h2 id="before-you-start">Before you start</h2>
      <p>Use a trusted Ubuntu or Debian VPS with sudo access, a domain, and ports 22, 80 and 443 available. You remain responsible for the host and application data. Replace the example hosts, app name and key filename throughout.</p>
      <h2 id="install-on-your-vps">1. Install on your VPS</h2>
      <pre><code>wget -O bootstrap.sh https://raw.githubusercontent.com/sshdock/sshdock/v1.1.1/scripts/bootstrap.sh
sudo SSHDOCK_TAG=v1.1.1 bash bootstrap.sh
sudo sshdock server domain set example.com</code></pre>
      <p>Point <code>sshdock.example.com</code> and <code>*.example.com</code> at your VPS. Always use the bootstrap script and assets from the same release tag.</p>
      <h2 id="authorize-your-key">2. Authorize your workstation</h2>
      <p>Send one public key through your existing administrator SSH connection. Keep its private key on your workstation, and verify the server host key through your trusted setup.</p>
      <pre><code>ssh ubuntu@server.example.com 'sudo sshdock ssh-keys add admin' &lt; ~/.ssh/id_ed25519.pub</code></pre>
      <p>Then check the installation on the server:</p>
      <pre><code>sudo sshdock diagnostics</code></pre>
      <h2 id="prepare-your-app">3. Prepare your app</h2>
      <p>In a committed Git repository, add exactly one supported root Compose file. Here is a small <code>compose.yaml</code>:</p>
      <pre><code>services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "127.0.0.1:3000:80"</code></pre>
      <p>Choose an unused host port for each app. Commit this file before pushing.</p>
      <h2 id="push-and-verify">4. Push and verify</h2>
      <pre><code>git remote add sshdock git@sshdock.example.com:my-app.git
git push sshdock main
ssh sshdock@sshdock.example.com apps health my-app
ssh sshdock@sshdock.example.com domains check my-app
curl --fail --show-error --connect-timeout 5 --max-time 20 https://my-app.example.com/</code></pre>
      <p>The first authorized push creates the app. SSHDock validates Compose, pulls images, builds, and waits for services to become ready. A successful deploy can create the default route when one public service and port can be inferred.</p>
      <aside class="callout"><strong>Git accepted is not app ready.</strong> The push prints a deployment ID and follows its log. Verify the deployment, services, route and public response separately. Ctrl-C detaches your client; an accepted deployment continues.</aside>
      <p>Open your dashboard:</p>
      <pre><code>ssh sshdock@sshdock.example.com</code></pre>
      <p>Next: <a href="docs.html?page=configuration">set app configuration</a> or learn the <a href="docs.html?page=deploying">deployment contract</a>. If the first deploy requires stored config, create the app explicitly on the server before setting those values.</p>
    `
  },
  {
    id: 'installation',
    title: 'Installation',
    group: 'Get started',
    description: 'Install the published release and establish trusted SSH access.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/INSTALL.md',
    body: `
      <p>Install SSHDock on one systemd-based Ubuntu or Debian VPS. The runtime uses Docker Engine, the Docker Compose plugin, Caddy, Git, OpenSSH and embedded SQLite.</p>
      <h2 id="host-requirements">Host requirements</h2>
      <ul><li>Linux amd64 or arm64, with root or sudo access.</li><li>Working administrator SSH access and a dedicated SSHDock data directory.</li><li>Ports 80 and 443 available for application traffic and certificate issuance.</li><li>A domain with a control-host record and wildcard app DNS pointing to this server.</li></ul>
      <p>Deployment access belongs to trusted owners. Compose workloads have access to the node's Docker runtime; SSHDock does not sandbox untrusted tenants.</p>
      <h2 id="install-v111">Install v1.1.1</h2>
      <pre><code>wget -O bootstrap.sh https://raw.githubusercontent.com/sshdock/sshdock/v1.1.1/scripts/bootstrap.sh
sudo SSHDOCK_TAG=v1.1.1 bash bootstrap.sh</code></pre>
      <p>The matching bootstrap downloads both binaries and verifies the release checksum. On a normal root install it installs missing runtime dependencies, configures the daemon and restricted SSH accounts, and adds the generated Caddy import.</p>
      <h2 id="connect-your-domain-and-key">Connect your domain and key</h2>
      <p>On the server:</p>
      <pre><code>sudo sshdock server domain set example.com</code></pre>
      <p>From your workstation, authorize a single public key using your existing administrator account:</p>
      <pre><code>ssh ubuntu@server.example.com 'sudo sshdock ssh-keys add admin' &lt; ~/.ssh/id_ed25519.pub</code></pre>
      <p>Do not copy the private key to the VPS or submit an entire <code>authorized_keys</code> file. The selected key can use Git deployment and the restricted operator interface.</p>
      <h2 id="verify-the-install">Verify the install</h2>
      <pre><code>sshdock version
sshdockd version
sudo sshdock diagnostics</code></pre>
      <p>From your workstation, open a fresh operator connection:</p>
      <pre><code>ssh sshdock@sshdock.example.com</code></pre>
      <p>The dashboard uses the host OpenSSH server. Keep your normal administrator connection for installation, host diagnostics and backup administration.</p>
      <h2 id="upgrade-an-existing-host">Upgrade an existing host</h2>
      <p>Record your configured paths, nondefault bootstrap settings, service drop-ins and external Caddy configuration. Pause mutations, finish deployments, and take separate host-state and application-data backups. Use the target release's matching bootstrap and explicitly resupply recorded settings: its invocation regenerates the service and SSH wrappers.</p>
      <aside class="callout"><strong>Verify preservation.</strong> After upgrading, check both versions, diagnostics, fresh SSH access, deployment history, active services and routes, public HTTPS, and actual saved application data. An installer exit alone does not establish these outcomes.</aside>
      <p>See the <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/INSTALL.md#upgrade-notes" target="_blank" rel="noreferrer">complete upgrade procedure</a> before changing an existing installation.</p>
    `
  },
  {
    id: 'deploying',
    title: 'Deploying apps',
    group: 'Get started',
    description: 'Let Git select the revision and Compose define the application.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/COMPOSE_SUPPORT.md',
    body: `
      <p>Your Git repository owns the application model. SSHDock checks out the accepted revision and delegates normal application semantics to Docker Compose.</p>
      <h2 id="one-root-compose-file">One root Compose file</h2>
      <p>Commit exactly one root file named <code>compose.yaml</code>, <code>compose.yml</code>, <code>docker-compose.yaml</code> or <code>docker-compose.yml</code>. Missing or multiple candidates are rejected rather than guessed. Follow the canonical guide for supported Compose file references and path boundaries.</p>
      <h2 id="push-to-remote-main">Push to remote main</h2>
      <pre><code>git remote add sshdock git@sshdock.example.com:my-app.git
git push sshdock main</code></pre>
      <p>Only the destination <code>refs/heads/main</code> is accepted. A different local source can explicitly target it:</p>
      <pre><code>git push sshdock feature:main</code></pre>
      <p>The first authorized push creates the app. The server permits one pending or active deployment per app; another push to that app is rejected before its ref changes. Other apps can queue. The daemon processes queued push deployments one at a time.</p>
      <h2 id="understand-the-attempt">Understand the attempt</h2>
      <p>An accepted push prints <code>deploy: queued dep_...</code> and follows redacted output. The daemon validates the effective model, pulls images, builds services and runs a bounded Compose <code>up -d --wait</code>. Services with health checks must become healthy; others must remain running.</p>
      <aside class="callout"><strong>The daemon owns accepted work.</strong> Ctrl-C or a lost connection detaches the client. It does not cancel deployment. Pending work survives daemon restart; interrupted active work is recorded as failed with retry guidance.</aside>
      <h2 id="reconnect-and-verify">Reconnect and verify</h2>
      <p>Replace the example ID with the one from your push:</p>
      <pre><code>ssh sshdock@sshdock.example.com deployments logs my-app dep_123 -f
ssh sshdock@sshdock.example.com deployments info my-app dep_123 --json
ssh sshdock@sshdock.example.com apps health my-app
ssh sshdock@sshdock.example.com domains check my-app</code></pre>
      <p>Check the exact attempt's state, then inspect services, routing and the public URL. A failed deploy leaves remote <code>main</code> at the accepted commit and may already have changed containers.</p>
      <h2 id="build-on-the-vps-or-in-ci">Build on the VPS or in CI</h2>
      <p>A Compose <code>build</code> runs on the VPS. For external builds, publish an image before pushing the exact source commit and reference the supplied revision metadata:</p>
      <pre><code>services:
  web:
    image: ghcr.io/example/app:\${SSHDOCK_GIT_SHA:?deploy a commit}
    restart: unless-stopped
    ports:
      - "127.0.0.1:3000:80"</code></pre>
      <p>Keep commit tags immutable and retain images needed for recovery. See the <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/EXTERNAL_BUILDS.md" target="_blank" rel="noreferrer">external-build guide</a> for registry credentials and a complete CI example.</p>
      <aside class="callout"><strong>Optional preflight.</strong> If the CLI is installed on your workstation, run <code>sshdock check</code> in the project directory before pushing. The current directory is the default. This checks the proposed model without deploying; it does not build images or prove runtime readiness.</aside>
    `
  },
  {
    id: 'configuration',
    title: 'Configuration',
    group: 'Daily operations',
    description: 'Keep values outside Git and make requirements explicit in Compose.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/CLI_COMMANDS.md#sshdock-config-set-app-key',
    body: `
      <p>Store app configuration through SSH, then reference it with normal Compose interpolation. SSHDock supplies stored values to the Compose process; your committed model decides which values reach each container.</p>
      <h2 id="declare-required-values">Declare required values</h2>
      <pre><code>services:
  web:
    environment:
      DATABASE_URL: \${DATABASE_URL:?set DATABASE_URL with sshdock config set}</code></pre>
      <p>A missing required value fails Compose validation before containers start. Keep real secrets out of the repository.</p>
      <h2 id="configure-before-your-first-push">Configure before your first push</h2>
      <p>Config commands require an existing app and do not create one. If the first deploy needs stored values, first create the app from a normal administrator session on the server:</p>
      <pre><code>sudo sshdock apps create my-app</code></pre>
      <p>From your workstation, store the value through stdin, inspect key names, and push:</p>
      <pre><code>ssh sshdock@sshdock.example.com config set my-app DATABASE_URL &lt; database-url.txt
ssh sshdock@sshdock.example.com config list my-app
git push sshdock main</code></pre>
      <p>Protect and remove temporary secret files according to your own secret-management process. For apps without required config, push-to-create remains the normal path.</p>
      <h2 id="update-a-running-app">Update a running app</h2>
      <pre><code>ssh sshdock@sshdock.example.com config import my-app &lt; .env.production
ssh sshdock@sshdock.example.com config keys my-app
ssh sshdock@sshdock.example.com apps redeploy my-app</code></pre>
      <p>Import reads <code>KEY=VALUE</code> lines; blank lines and comments are ignored. Setting, importing or unsetting values does not restart or redeploy containers. Restart also does not apply changed values: explicitly redeploy current remote <code>main</code>.</p>
      <h2 id="inspect-without-revealing">Inspect without revealing</h2>
      <p><code>config list</code> shows key metadata and <code>config keys</code> shows names. <code>config get</code> deliberately reveals a value; use it only when needed. Normal inspection and deployment output redact known stored values, but arbitrary transformed secrets are outside that guarantee.</p>
      <h2 id="encryption-and-backups">Encryption and backups</h2>
      <p>Config values are encrypted in SQLite using a host-local key, by default <code>/var/lib/sshdock/config.key</code>. Keep the database and key together for recovery. This protects a database-only leak; it does not protect against a compromised host, root, Docker or a malicious trusted workload.</p>
      <aside class="callout"><strong>Encrypted config does not mean encrypted archives.</strong> Host-state backups include the decryption key and are unencrypted. Protect and encrypt backup files before storing them elsewhere.</aside>
      <p>Operational variables such as <code>SSHDOCK_*</code>, <code>COMPOSE_*</code>, <code>DOCKER_*</code>, <code>PATH</code> and <code>HOME</code> are reserved. See the <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/CLI_COMMANDS.md#sshdock-config-set-app-key" target="_blank" rel="noreferrer">full config reference</a> for restrictions and older scoped-config migration.</p>
    `
  },
  {
    id: 'operations',
    title: 'Daily operations',
    group: 'Daily operations',
    description: 'Inspect, restart and work inside your apps over SSH.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/CLI_COMMANDS.md#sshdockd-operator',
    body: `
      <p>Use the SSH dashboard for an overview, or issue the same supported app operations as direct SSH commands. Host administration stays in your normal administrator session.</p>
      <h2 id="open-the-dashboard">Open the dashboard</h2>
      <pre><code>ssh sshdock@sshdock.example.com</code></pre>
      <figure class="tui-figure"><img src="tui-preview.svg" alt="SSHDock terminal dashboard with an app list on the left and Summary, Services, Routes, Releases, Deploys, Events and Logs tabs on the right." width="928" height="491"><figcaption>SSHDock TUI · sample data.</figcaption></figure>
      <p>Select an app with arrows or <code>j</code>/<code>k</code>, filter with <code>/</code>, and switch detail tabs with <code>Tab</code>. Press <code>a</code> for app actions, <code>r</code> to refresh and <code>q</code> to quit. The tabs cover Summary, Services, Routes, Releases, Deploys, Events and Logs.</p>
      <h2 id="read-health-before-acting">Read health before acting</h2>
      <pre><code>ssh sshdock@sshdock.example.com apps list
ssh sshdock@sshdock.example.com apps health my-app</code></pre>
      <p>Read desired remote <code>main</code>, the latest attempt and the last successful attempt as separate facts. A historical success does not prove its containers are still serving. Health also reports services from the current worktree's Compose project and active route observations.</p>
      <h2 id="choose-the-right-lifecycle-command">Choose the right lifecycle command</h2>
      <pre><code>ssh sshdock@sshdock.example.com apps stop my-app
ssh sshdock@sshdock.example.com apps start my-app
ssh sshdock@sshdock.example.com apps restart my-app web</code></pre>
      <p>Stop preserves existing containers, networks and volumes. Start uses existing containers; it does not recreate missing ones. Restart uses Compose restart. None of these applies changed Compose definitions or config. To apply those changes, push a new revision or redeploy current remote <code>main</code>.</p>
      <p>Use a Compose restart policy such as <code>restart: unless-stopped</code> for services that should return after Docker or host reboot. The daemon processes deployment work; Docker owns container restart behavior.</p>
      <h2 id="run-a-command">Run a command</h2>
      <pre><code>ssh -tt sshdock@sshdock.example.com apps exec my-app web -- sh
ssh sshdock@sshdock.example.com apps run my-app web -- ./bin/migrate up</code></pre>
      <p><code>exec</code> uses an existing service container. <code>run</code> creates a removable one-off container from its service definition. The required <code>--</code> separates the container command from SSHDock options. Review data-changing commands and migration compatibility before running them.</p>
      <h2 id="remove-an-app">Remove an app</h2>
      <p>From your administrator session, use the interactive name confirmation:</p>
      <pre><code>sudo sshdock apps remove my-app</code></pre>
      <p>This removes managed containers, app repositories/worktrees, live metadata and routes. Docker volumes are preserved, and removal audit history remains readable. Preserved volumes are not a substitute for tested backups.</p>
      <aside class="callout"><strong>A trusted-owner tool.</strong> There is no web control plane or team access model. Give deploy/operator keys only to people you trust with the node and its data.</aside>
    `
  },
  {
    id: 'domains',
    title: 'Domains & HTTPS',
    group: 'Daily operations',
    description: 'Route a loopback-published service through Caddy.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/INSTALL.md#caddy-requirement',
    body: `
      <p>Caddy runs on the host and proxies to a published loopback port in your Compose application. SSHDock stores routing intent, renders its generated Caddy configuration and requests a reload.</p>
      <h2 id="set-up-dns">Set up DNS</h2>
      <p>Set a base domain on the server:</p>
      <pre><code>sudo sshdock server domain set example.com</code></pre>
      <p>Create DNS records for <code>sshdock.example.com</code> and <code>*.example.com</code> pointing at the VPS. Keep ports 80 and 443 reachable for application traffic and ACME. DNS, provider ingress and host firewall rules remain your responsibility.</p>
      <h2 id="publish-a-loopback-port">Publish a loopback port</h2>
      <pre><code>services:
  web:
    image: nginx:alpine
    restart: unless-stopped
    ports:
      - "127.0.0.1:3000:80"</code></pre>
      <p>Here <code>3000</code> is the host port Caddy reaches and <code>80</code> is the container port. Pick an unused host port. Loopback binding keeps direct application traffic behind Caddy.</p>
      <h2 id="use-the-default-route">Use the default route</h2>
      <p>After a successful deploy, an app without an existing domain can receive <code>my-app.example.com</code> when a public service and one TCP host port can be inferred safely. Worker-only or ambiguous models may deploy successfully without an automatic route; inspect the routing guidance instead of guessing a public service.</p>
      <h2 id="attach-a-custom-domain">Attach a custom domain</h2>
      <p>Point the custom domain at the VPS, then run locally on the server:</p>
      <pre><code>sudo sshdock domains attach my-app web app.example.com --port 3000
sudo sshdock domains list my-app
sudo sshdock domains check my-app</code></pre>
      <p><code>--port</code> is the loopback-published host port. SSHDock validates generated config and reloads the main Caddy configuration, preserving administrator-owned imports and sites. Keep Caddy's admin endpoint local to the host.</p>
      <h2 id="verify-three-separate-layers">Verify three separate layers</h2>
      <pre><code>ssh sshdock@sshdock.example.com apps health my-app
ssh sshdock@sshdock.example.com domains check my-app
curl --fail --show-error --connect-timeout 5 --max-time 20 https://app.example.com/</code></pre>
      <p>Service readiness, matching active Caddy config and public HTTPS are independent observations. Caddy may still be obtaining a certificate after a configuration reload. If a check fails, inspect DNS, listeners and Caddy logs before changing deployment history.</p>
      <aside class="callout"><strong>No traffic-switch guarantee.</strong> A failed Compose replacement can change running containers even when an older route remains. Successful deployment does not promise zero downtime or working public DNS/TLS.</aside>
    `
  },
  {
    id: 'logs-history',
    title: 'Logs & history',
    group: 'Daily operations',
    description: 'Find the right evidence for a build failure or a runtime problem.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/CLI_COMMANDS.md#sshdock-deployments-logs-app-deployment-id--f',
    body: `
      <p>Deployment output and container output answer different questions. Start with the one that matches the failure, then use the attempt ID to correlate history.</p>
      <h2 id="inspect-deployment-output">Inspect deployment output</h2>
      <pre><code>ssh sshdock@sshdock.example.com deployments list my-app
ssh sshdock@sshdock.example.com deployments logs my-app dep_123 -f</code></pre>
      <p>Use the exact ID printed by your push. These durable logs contain deployment-stage and Compose output. Omitting the ID selects the latest deployment log; that may belong to a different operation.</p>
      <p>Ctrl-C stops following without cancelling an accepted deployment. Logs are redacted for known stored config values, capped at 10 MiB each with a truncation marker, and retained for the newest 20 logs per app. Attempt history can outlive its retained log.</p>
      <h2 id="inspect-container-output">Inspect container output</h2>
      <pre><code>ssh sshdock@sshdock.example.com logs my-app web --tail 200 -f</code></pre>
      <p>This reads Compose service logs for the current worktree project. The default tail is 100 lines. In the dashboard, open Logs for the selected app; press <code>f</code> to toggle follow and <code>u</code>/<code>d</code> to scroll.</p>
      <h2 id="read-releases-attempts-and-events">Read releases, attempts and events</h2>
      <ul><li><strong>Release:</strong> one stable record for an app commit.</li><li><strong>Deployment:</strong> one push or redeploy attempt, with its own ID, timing and result. Retrying a commit creates another attempt against the same release.</li><li><strong>Event:</strong> an audit record for deployment, routing, warnings or operations.</li></ul>
      <pre><code>ssh sshdock@sshdock.example.com releases list my-app
ssh sshdock@sshdock.example.com deployments info my-app dep_123 --json
ssh sshdock@sshdock.example.com events list my-app</code></pre>
      <h2 id="read-health-identities">Read health identities</h2>
      <p><code>current_main_commit</code> is desired Git state. The latest attempt is the newest recorded work and may have failed. The last successful attempt is historical success, not a live availability probe. Check observed service and route results alongside those identities.</p>
      <aside class="callout"><strong>Query success is not deployment success.</strong> A successful <code>deployments info --json</code> lookup exits 0 even when its attempt is pending or failed. Inspect <code>data.status</code> and verify the exact ID and commit.</aside>
      <h2 id="automate-inspection">Automate inspection</h2>
      <p>Supported read-only commands accept a trailing <code>--json</code>. Check <code>schema_version</code>, the process exit, <code>error</code> and the result fields; keep SSH stderr separate. Use the <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/JSON_OUTPUT.md#ci-polling-example" target="_blank" rel="noreferrer">bounded exact-ID polling recipe</a> for CI instead of treating an accepted push as completion.</p>
    `
  },
  {
    id: 'recovery',
    title: 'Recover a deployment',
    group: 'Recovery',
    description: 'Diagnose first, then retry or deliberately select a compatible Git revision.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/RECOVERY.md',
    body: `
      <p>A failed deployment is a recorded attempt, not an automatic rollback. Remote <code>main</code> remains the desired revision. Recovery is an explicit operator decision using the same Git and Compose path.</p>
      <h2 id="identify-what-failed">Identify what failed</h2>
      <pre><code>ssh sshdock@sshdock.example.com apps health my-app
ssh sshdock@sshdock.example.com deployments list my-app
ssh sshdock@sshdock.example.com deployments logs my-app dep_123
ssh sshdock@sshdock.example.com events list my-app</code></pre>
      <p>Replace the example ID with the failed attempt. Read its stage, detail, changed state, suggested fix and retry guidance. Compare desired <code>main</code>, the latest attempt, the last success, current services and active routes.</p>
      <h2 id="retry-the-same-revision">Retry the same revision</h2>
      <p>If the desired commit is still correct, fix its prerequisite first—for example required config or registry access—then explicitly redeploy:</p>
      <pre><code>ssh sshdock@sshdock.example.com apps redeploy my-app
ssh sshdock@sshdock.example.com deployments list my-app</code></pre>
      <p>Redeploy selects current remote <code>main</code> and records a new attempt. An unchanged-ref Git push does not create an attempt. Identify the new redeploy ID by its commit and invocation time; do not reuse the earlier attempt's result.</p>
      <h2 id="select-an-earlier-revision">Select an earlier revision</h2>
      <p>Before choosing older code, review database migrations, stored config, external resources and image availability. Preserved volumes do not undo data changes, and the last successful commit is only a candidate.</p>
      <ol><li>Capture desired remote <code>main</code> and attempt identities in one health snapshot.</li><li>Resolve the reviewed local commit or tag to a full commit SHA and inspect its changes.</li><li>Confirm that exact target and push to remote <code>main</code> with an explicit expected-main lease.</li><li>If the lease is stale or the app is busy, stop and inspect again. Do not fall back to an unguarded force push.</li></ol>
      <p>Use the <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/RECOVERY.md#identify-the-desired-revision-and-recovery-target" target="_blank" rel="noreferrer">complete reviewed-target and lease procedure</a>. The Git receive account supports pushes, not fetch or <code>git ls-remote</code>.</p>
      <h2 id="verify-the-new-attempt">Verify the new attempt</h2>
      <p>Copy the new ID from <code>deploy: queued</code> output or identify the exact redeploy attempt. Verify its ID, target commit and terminal state, then inspect health, active routes and public behavior:</p>
      <pre><code>ssh sshdock@sshdock.example.com deployments logs my-app dep_456 -f
ssh sshdock@sshdock.example.com deployments info my-app dep_456 --json
ssh sshdock@sshdock.example.com apps health my-app
ssh sshdock@sshdock.example.com domains check my-app
curl --fail --show-error --connect-timeout 5 --max-time 20 https://my-app.example.com/</code></pre>
      <p>Verify saved application records, a new committed write and expected version behavior separately.</p>
      <aside class="callout"><strong>Recovery can also fail.</strong> There is no automatic migration reversal, zero-downtime switch or availability SLA. A failed replacement may already have changed containers. Restore data only through a reviewed, tested application-data recovery procedure.</aside>
    `
  },
  {
    id: 'backups',
    title: 'Back up & restore',
    group: 'Recovery',
    description: 'Protect host state and application data under their separate consistency windows.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/APPLICATION_DATA_BACKUPS.md',
    body: `
      <p>A complete recovery plan needs both SSHDock host state and application data. These are manual operations through a normal administrator session, with separately coordinated consistency windows.</p>
      <h2 id="choose-the-right-backup">Choose the right backup</h2>
      <ul><li><strong>Host state:</strong> SQLite metadata, config key, managed Git/worktrees, key state, Caddy files and a volume inventory. It does not copy volume contents.</li><li><strong>File volume:</strong> one ordinary local named volume's supported files/directories, numeric ownership and basic modes, with every writer stopped.</li><li><strong>Database-native:</strong> use tools such as PostgreSQL <code>pg_dump</code> for a consistent database snapshot; coordinate other files and services separately.</li></ul>
      <aside class="callout"><strong>All these archives are unencrypted and may contain secrets.</strong> A host-state archive includes the config decryption key. Protect archive files, encrypt off-host copies with your own tools and restore only trusted archives.</aside>
      <h2 id="capture-host-state">Capture host state</h2>
      <p>Finish deployments and pause pushes, CI and operator mutations. This is a file copy, not an online SQLite snapshot. On the server:</p>
      <pre><code>sudo systemctl stop sshdockd
sudo sshdock backup create --output /root/sshdock-backup.tar.gz
sudo sshdock backup inspect /root/sshdock-backup.tar.gz
sudo systemctl start sshdockd</code></pre>
      <p>App containers keep running. If backup creation fails, inspect the error and deliberately restore daemon operation. The host archive records volume inventory only; <code>--include-volumes</code> is unsupported.</p>
      <h2 id="export-a-stopped-writer-volume">Export a stopped-writer volume</h2>
      <p>Record the exact volume, Compose mapping, image version and expected data. Stop every writer, including other apps, host processes and external actors; keep pushes and writes paused until export finishes. From the server:</p>
      <pre><code>sudo sshdock apps stop my-app
sudo docker volume inspect sshdock_my-app_data
sudo docker ps --filter volume=sshdock_my-app_data
sudo sshdock backup volume export sshdock_my-app_data --output /root/my-app-data.tar.gz --quiesced
sudo sshdock backup volume inspect /root/my-app-data.tar.gz</code></pre>
      <p>Confirm the actual volume name rather than assuming it. The command rejects observed running read/write consumers, but <code>--quiesced</code> is your acknowledgement of the remaining writer responsibility. Restart writers only after a successful export and inspection.</p>
      <h2 id="restore-into-a-fresh-target">Restore into a fresh target</h2>
      <pre><code>sudo sshdock backup volume inspect /root/my-app-data.tar.gz
sudo sshdock backup volume restore /root/my-app-data.tar.gz --name my-app-restored-data</code></pre>
      <p>Every existing destination volume is refused, even an empty one. Restore validates the archive and creates a new volume; it does not rewrite Compose, deploy containers or switch traffic. Deliberately map the restored volume in committed Compose, deploy and verify saved data, a new write and persistence after restart.</p>
      <h2 id="plan-full-host-recovery">Plan full-host recovery</h2>
      <p>Retain off-host backups and record version, configured paths, desired commits, image pins and volume mappings. Use a compatible destination and the same configured directory layout, because app rows contain absolute paths. Restore host state with the daemon stopped, then restore application data into fresh targets before resuming writers.</p>
      <p>File-volume archives do not support arbitrary links, special files or all filesystem metadata. Do not copy a live PostgreSQL data directory as an online backup. Follow the canonical <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/APPLICATION_DATA_BACKUPS.md#postgresql-logical-dump-and-restore" target="_blank" rel="noreferrer">PostgreSQL logical recipe</a> and <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/APPLICATION_DATA_BACKUPS.md#restore-on-a-cleared-or-independent-host" target="_blank" rel="noreferrer">full destination checklist</a>. Test restoration before relying on it.</p>
    `
  },
  {
    id: 'reference',
    title: 'Reference',
    group: 'Reference',
    description: 'The complete command contract and deeper guides, when you need them.',
    source: 'https://github.com/sshdock/sshdock/blob/v1.1.1/docs/CLI_COMMANDS.md',
    body: `
      <p>These guides cover the published v1.1.1 Git, SSH and Compose workflow. The repository remains the canonical source for complete syntax, limits and version-specific behavior.</p>
      <h2 id="find-command-help">Find command help</h2>
      <pre><code>sshdock help
sshdock apps --help
sshdock help config
ssh sshdock@sshdock.example.com help</code></pre>
      <p>Local CLI help lists user and host-admin commands. Restricted SSH help lists the operations available through the operator account. Installation, key administration, app creation, host diagnostics and backup administration use the local CLI in a normal administrator session.</p>
      <h2 id="core-guides">Core guides</h2>
      <ul>
        <li><a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/CLI_COMMANDS.md" target="_blank" rel="noreferrer">CLI command reference</a> — full syntax, output, lifecycle and daemon entry points.</li>
        <li><a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/COMPOSE_SUPPORT.md" target="_blank" rel="noreferrer">Compose support</a> — file selection, native validation, host coupling and routing inference.</li>
        <li><a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/JSON_OUTPUT.md" target="_blank" rel="noreferrer">Structured inspection</a> — schema version 1, result meanings and exact-ID CI polling.</li>
        <li><a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/PREFLIGHT.md" target="_blank" rel="noreferrer">Read-only preflight</a> — blockers, warnings, unknowns and what is not exercised.</li>
        <li><a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/TROUBLESHOOTING.md" target="_blank" rel="noreferrer">Troubleshooting</a> — installation, SSH, deployment and routing diagnosis.</li>
      </ul>
      <h2 id="examples-and-builds">Examples and builds</h2>
      <p>Start from a Compose app you understand. The <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/EXAMPLES.md" target="_blank" rel="noreferrer">example catalog</a> contains framework probes, software recipes, databases and feature labs. These are examples of the same Compose contract, not separate runtime plugins. The <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/EXTERNAL_BUILDS.md" target="_blank" rel="noreferrer">external-build guide</a> shows commit-tagged images built in your CI.</p>
      <h2 id="security-and-responsibility">Security and responsibility</h2>
      <p>Deploy keys belong to trusted owners. Review public port bindings, privileged containers, host networking, bind mounts, Docker socket access and global/external volumes. Warnings make coupling visible; they do not isolate workloads. Read the <a href="https://github.com/sshdock/sshdock/blob/v1.1.1/docs/SECURITY.md" target="_blank" rel="noreferrer">security boundary</a> before granting access.</p>
      <p>SSHDock is Apache-2.0 software. Manual operations run on your VPS without an SSHDock Cloud account or subscription. You own host maintenance, availability, data consistency and recovery decisions.</p>
      <h2 id="releases-and-contributing">Releases and contributing</h2>
      <ul>
        <li><a href="https://github.com/sshdock/sshdock/releases/tag/v1.1.1" target="_blank" rel="noreferrer">v1.1.1 release and assets</a> — use the matching installer and architecture archive.</li>
        <li><a href="https://github.com/sshdock/sshdock/blob/main/docs/V1_1_RELEASE.md" target="_blank" rel="noreferrer">Release and upgrade notes</a> — patch scope and operational limits.</li>
        <li><a href="https://github.com/sshdock/sshdock/blob/main/AGENTS.md" target="_blank" rel="noreferrer">Contributor contract</a> and <a href="https://github.com/sshdock/sshdock/blob/main/docs/TESTING.md" target="_blank" rel="noreferrer">testing guide</a> — current-source development rules.</li>
        <li><a href="https://github.com/sshdock/sshdock/issues" target="_blank" rel="noreferrer">GitHub issues</a> — report a concrete problem or discuss a proposed change.</li>
      </ul>
      <aside class="callout"><strong>Keep the scope small.</strong> One node, Compose applications and SSH operations. No web control panel, multi-node scheduling, automatic rollback, scheduled backup service or uptime guarantee is included.</aside>
    `
  }
];
