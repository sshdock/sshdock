# SSHDock website

Static landing page and task-based documentation for the published v1.1.1 release. The versioned repository documentation remains the canonical command reference.

Preview from the repository root:

```bash
python3 -m http.server 8080 --directory website/public
```

Open `http://localhost:8080`. Any static host can serve `website/public`; no build step or package installation is required.

Check JavaScript syntax, documentation links and required assets with Node.js:

```bash
cd website
node --check public/app.js
node --check public/docs-data.js
node check.mjs
```

`public/docs-data.js` contains the guides and their versioned source links. Update commands and release links together when changing the documented version. `public/tui-preview.svg` uses SSHDock's renderer with synthetic app data; the landing terminal is an example session.

Hosting credentials and provider configuration are kept outside this directory. The website has no backend or runtime controls.
