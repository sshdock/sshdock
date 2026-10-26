(() => {
  'use strict';
  const escapeHTML = (value) => String(value).replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

  document.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-copy]');
    if (!button) return;
    const code = button.closest('[data-code]')?.querySelector('code');
    if (!code) return;
    const status = document.getElementById('copy-status');
    try {
      await navigator.clipboard.writeText(code.textContent.trim());
      button.textContent = 'Copied ✓';
      status.textContent = 'Code copied to clipboard.';
    } catch {
      const range = document.createRange();
      range.selectNodeContents(code);
      window.getSelection().removeAllRanges();
      window.getSelection().addRange(range);
      button.textContent = 'Selected';
      status.textContent = 'Clipboard unavailable. Code selected; use your copy shortcut.';
    }
    setTimeout(() => { if (button.isConnected) button.textContent = 'Copy'; }, 2000);
  });

  const terminal = document.getElementById('terminal-panel');
  if (terminal) {
    const deploy = terminal.innerHTML;
    const sessions = {
      deploy,
      operate: `<div class="terminal-line"><span class="prompt">❯</span> ssh sshdock@sshdock.example.com</div><img class="terminal-tui" src="tui-preview.svg" alt="Actual SSHDock terminal dashboard rendered with sample app data."><a class="terminal-guide" href="docs.html?page=operations">Explore the SSH TUI ↗</a>`,
      recover: `<div class="terminal-line"><span class="prompt">❯</span> ssh sshdock@sshdock.example.com</div><div class="terminal-muted terminal-indent">deployments list my-app</div><div class="terminal-gap"></div><div class="terminal-mini-table inspect-table"><span>ATTEMPT</span><span>COMMIT</span><span>STATUS</span><span class="terminal-bright">dep_…</span><span>abc123</span><span class="terminal-check">succeeded</span></div><div class="terminal-gap"></div><div class="terminal-line"><span class="prompt">❯</span> ssh sshdock@sshdock.example.com</div><div class="terminal-muted terminal-indent">deployments logs my-app -f</div><div class="terminal-gap"></div><div class="terminal-muted">Reconnect to durable deployment output.</div><div class="terminal-cursor" aria-hidden="true">▍</div>`
    };
    const tabs = [...document.querySelectorAll('[data-terminal]')];
    const select = (tab) => {
      tabs.forEach((item) => { const selected = item === tab; item.setAttribute('aria-selected', String(selected)); item.tabIndex = selected ? 0 : -1; });
      terminal.innerHTML = sessions[tab.dataset.terminal];
      terminal.setAttribute('aria-labelledby', tab.id);
    };
    tabs.forEach((tab, index) => {
      tab.addEventListener('click', () => select(tab));
      tab.addEventListener('keydown', (event) => {
        let next;
        if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
        if (event.key === 'ArrowLeft') next = (index + tabs.length - 1) % tabs.length;
        if (event.key === 'Home') next = 0;
        if (event.key === 'End') next = tabs.length - 1;
        if (next !== undefined) { event.preventDefault(); select(tabs[next]); tabs[next].focus(); }
      });
    });
  }

  const pages = window.SSHDOCK_DOCS;
  if (!pages?.length || !document.getElementById('doc-body')) return;
  const requested = new URLSearchParams(location.search).get('page') || 'quick-start';
  const page = pages.find((entry) => entry.id === requested) || pages[0];
  const pageURL = (entry) => `docs.html?page=${encodeURIComponent(entry.id)}`;
  document.title = `${page.title} — SSHDock Docs`;
  document.getElementById('doc-title').textContent = page.title;
  document.querySelector('meta[name=description]').content = page.description;
  document.getElementById('doc-source').href = page.source;
  const body = document.getElementById('doc-body');
  body.innerHTML = page.body;
  body.querySelectorAll('pre').forEach((pre) => {
    const wrapper = document.createElement('div');
    wrapper.className = 'doc-code';
    wrapper.dataset.code = '';
    pre.before(wrapper);
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'copy-button';
    button.dataset.copy = '';
    button.textContent = 'Copy';
    button.setAttribute('aria-label', 'Copy code snippet');
    wrapper.append(button, pre);
  });
  const headings = [...body.querySelectorAll('h2')];
  document.getElementById('doc-toc').innerHTML = headings.map((h, i) => {
    if (!h.id) h.id = `section-${i + 1}`;
    return `<a href="#${escapeHTML(h.id)}">${escapeHTML(h.textContent)}</a>`;
  }).join('');
  const navigation = document.getElementById('docs-nav');
  const search = document.getElementById('docs-search');
  const plainText = pages.map((entry) => {
    const text = document.createElement('div');
    text.innerHTML = entry.body;
    return `${entry.title} ${entry.description} ${text.textContent}`.toLowerCase();
  });
  const renderNavigation = () => {
    const term = search.value.trim().toLowerCase();
    const filtered = pages.filter((entry, index) => !term || plainText[index].includes(term));
    const groups = [...new Set(filtered.map((entry) => entry.group))];
    navigation.innerHTML = groups.map((group) => `<div class="nav-group"><p>${escapeHTML(group)}</p>${filtered.filter((entry) => entry.group === group).map((entry) => `<a href="${pageURL(entry)}"${entry.id === page.id ? ' class="current" aria-current="page"' : ''}>${escapeHTML(entry.title)}${entry.id === page.id ? '<span aria-hidden="true">↗</span>' : ''}</a>`).join('')}</div>`).join('') || '<p class="search-empty">No guide found. Try “logs”, “config”, or “backup”.</p>';
    document.getElementById('search-summary').textContent = term ? `${filtered.length} guides found.` : '';
  };
  search.addEventListener('input', renderNavigation);
  document.addEventListener('keydown', (event) => {
    if (event.key === '/' && !/INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName) && !event.metaKey && !event.ctrlKey && !event.altKey) {
      event.preventDefault();
      if (matchMedia('(max-width: 900px)').matches) setMenu(true);
      search.focus();
    }
    if (event.key === 'Escape') { search.value = ''; renderNavigation(); setMenu(false); search.blur(); }
  });
  const menuButton = document.querySelector('.docs-menu-toggle');
  const setMenu = (open) => {
    document.getElementById('docs-sidebar').classList.toggle('open', open);
    menuButton.setAttribute('aria-expanded', String(open));
  };
  menuButton.addEventListener('click', () => setMenu(menuButton.getAttribute('aria-expanded') !== 'true'));
  renderNavigation();
  const index = pages.indexOf(page);
  document.getElementById('doc-pagination').innerHTML = [pages[index - 1], pages[index + 1]].map((entry, side) => entry ? `<a class="${side ? 'next' : 'previous'}" href="${pageURL(entry)}" aria-label="${side ? 'Next' : 'Previous'}: ${escapeHTML(entry.title)}"><strong>${side ? '' : '← '}${escapeHTML(entry.title)}${side ? ' →' : ''}</strong></a>` : '<span></span>').join('');
  if (location.hash) requestAnimationFrame(() => document.getElementById(decodeURIComponent(location.hash.slice(1)))?.scrollIntoView());
})();
