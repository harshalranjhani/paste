(() => {
  document.documentElement.classList.add('js');
  function syncPreviewTheme() {
    const frame = document.querySelector('iframe[data-preview-kind="markdown"]');
    if (!frame) return;
    const documentHTML = frame.srcdoc.replace(/<html data-theme="(?:light|dark)">/, '<html data-theme="' + document.documentElement.dataset.theme + '">');
    if (frame.srcdoc !== documentHTML) frame.srcdoc = documentHTML;
  }
  const themeToggle = document.getElementById('theme-toggle');
  function updateThemeToggle() {
    const label = document.documentElement.dataset.theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode';
    themeToggle.setAttribute('aria-label', label);
    themeToggle.title = label;
    syncPreviewTheme();
  }
  themeToggle.addEventListener('click', () => {
    const theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = theme;
    try { localStorage.setItem('paste-theme', theme); } catch {}
    updateThemeToggle();
  });
  window.addEventListener('pageshow', () => {
    try {
      const theme = localStorage.getItem('paste-theme');
      if (theme === 'light' || theme === 'dark') document.documentElement.dataset.theme = theme;
    } catch {}
    updateThemeToggle();
  });
  updateThemeToggle();
  const toast = document.getElementById('toast');
  let toastTimer;
  function notify(message) {
    toast.textContent = message;
    toast.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { toast.hidden = true; }, 4000);
  }

  document.querySelectorAll('[data-copy], [data-copy-file]').forEach(button => {
    button.addEventListener('click', async () => {
      button.disabled = true;
      try {
        let text = button.dataset.copy;
        if (button.dataset.copyFile) {
          const response = await fetch(button.dataset.copyFile, { credentials: 'same-origin' });
          if (!response.ok) throw new Error('File unavailable');
          text = await response.text();
        }
        if (!navigator.clipboard) throw new Error('Clipboard unavailable');
        await navigator.clipboard.writeText(text);
        notify(button.dataset.copyFile ? 'File contents copied.' : 'Copied to clipboard.');
      } catch {
        notify('Could not copy. Select the text or use the Raw link to copy manually.');
      } finally {
        button.disabled = false;
      }
    });
  });

  document.querySelectorAll('[data-confirm]').forEach(form => {
    form.addEventListener('submit', event => {
      if (!window.confirm(form.dataset.confirm)) event.preventDefault();
    });
  });

  document.querySelector('[data-back]')?.addEventListener('click', event => {
    if (history.length > 1) { event.preventDefault(); history.back(); }
  });

  const files = document.getElementById('files');
  if (files) {
    const add = document.getElementById('add-file');
    function updateFiles() {
      const rows = files.querySelectorAll('.file-row');
      document.getElementById('file-count').textContent = rows.length + (rows.length === 1 ? ' file' : ' files');
      rows.forEach((row, index) => {
        const path = row.querySelector('[name="path"]');
        path.setAttribute('aria-label', 'File path ' + (index + 1));
        row.querySelector('textarea').setAttribute('aria-label', 'File contents ' + (index + 1));
        const remove = row.querySelector('.remove-file');
        remove.disabled = rows.length === 1;
        remove.setAttribute('aria-label', 'Remove file ' + (index + 1));
      });
    }
    add.addEventListener('click', () => {
      const row = files.querySelector('.file-row').cloneNode(true);
      row.querySelectorAll('input, textarea').forEach(input => { input.value = ''; });
      files.appendChild(row);
      updateFiles();
      row.querySelector('input').focus();
    });
    files.addEventListener('click', event => {
      const remove = event.target.closest('.remove-file');
      if (!remove || files.children.length === 1) return;
      const row = remove.closest('.file-row');
      if (row.querySelector('textarea').value && !window.confirm('Remove this file and its contents?')) return;
      row.remove();
      updateFiles();
      add.focus();
    });
    updateFiles();
  }

  document.querySelectorAll('time[datetime]').forEach(time => {
    const date = new Date(time.dateTime);
    if (Number.isNaN(date.getTime())) return;
    time.textContent = date.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
    time.title = date.toLocaleString();
  });

  const pane = document.getElementById('code-pane');
  const fullscreen = document.getElementById('fullscreen');
  if (fullscreen && document.fullscreenEnabled) {
    fullscreen.hidden = false;
    fullscreen.addEventListener('click', async () => {
      try {
        if (document.fullscreenElement) await document.exitFullscreen();
        else await document.getElementById('viewer-workspace').requestFullscreen();
      } catch { notify('Fullscreen is unavailable in this browser.'); }
    });
    document.addEventListener('fullscreenchange', () => {
      const active = Boolean(document.fullscreenElement);
      const label = active ? 'Exit fullscreen' : 'Enter fullscreen';
      fullscreen.setAttribute('aria-label', label);
      fullscreen.title = label;
      fullscreen.setAttribute('aria-pressed', String(active));
      (document.fullscreenElement || document.body).appendChild(toast);
    });
  }
  document.getElementById('wrap-lines')?.addEventListener('click', event => {
    const wrapped = pane.classList.toggle('wrap');
    event.currentTarget.setAttribute('aria-pressed', String(wrapped));
  });

  const tree = document.getElementById('file-tree');
  if (pane) {
    const links = tree ? [...tree.querySelectorAll('.file-link')] : [];
    const picker = document.getElementById('file-picker');
    const codeMain = document.querySelector('.code-main');
    const syntaxForm = document.getElementById('syntax-form');
    const syntax = document.getElementById('syntax-language');
    const viewCode = document.getElementById('view-code');
    const viewPreview = document.getElementById('view-preview');
    let controller;
    let requestNumber = 0;
    async function openFile(link, pushHistory = true, language = '', mode = codeMain.dataset.viewMode) {
      const url = new URL(link ? link.href : location.href);
      url.searchParams.delete('partial');
      if (language) url.searchParams.set('language', language);
      else url.searchParams.delete('language');
      url.searchParams.set('view', mode);
      const partialURL = new URL(url);
      partialURL.searchParams.set('partial', '1');
      controller?.abort();
      controller = new AbortController();
      const number = ++requestNumber;
      pane.setAttribute('aria-busy', 'true');
      document.getElementById('copy-contents').disabled = true;
      try {
        const response = await fetch(partialURL, { credentials: 'same-origin', signal: controller.signal });
        if (response.status === 401 || response.status === 403 || response.status === 410) { location.assign(url); return; }
        if (!response.ok) throw new Error('File unavailable');
        const code = await response.text();
        if (number !== requestNumber) return;
        pane.innerHTML = code;
        pane.scrollTop = 0;
        pane.scrollLeft = 0;
        syncPreviewTheme();
        if (link) {
          links.forEach(item => {
            const selected = item === link;
            item.setAttribute('aria-selected', String(selected));
            if (selected) item.setAttribute('aria-current', 'page');
            else item.removeAttribute('aria-current');
          });
          let ancestor = link.closest('details');
          while (ancestor) { ancestor.open = true; ancestor = ancestor.parentElement.closest('details'); }
          document.getElementById('selected-path').textContent = link.dataset.path;
          document.getElementById('file-size').textContent = link.dataset.size;
          const raw = '/api/v1/pastes/' + location.pathname.split('/').pop() + '/files/' + link.dataset.fileId + '/raw';
          document.getElementById('raw-link').href = raw;
          document.getElementById('copy-contents').dataset.copyFile = raw;
          picker.value = link.getAttribute('href');
          syntaxForm.elements.file_id.value = link.dataset.fileId;
        }
        syntax.value = response.headers.get('X-Syntax-Language') || '';
        codeMain.dataset.language = syntax.value;
        mode = response.headers.get('X-Viewer-Mode') || 'code';
        codeMain.dataset.viewMode = mode;
        url.searchParams.set('view', mode);
        viewPreview.hidden = !response.headers.get('X-File-Preview');
        document.getElementById('wrap-lines').hidden = mode === 'preview';
        syntaxForm.hidden = mode === 'preview';
        for (const [control, value] of [[viewCode, 'code'], [viewPreview, 'preview']]) {
          const target = new URL(url);
          target.searchParams.set('view', value);
          control.href = target;
          if (value === mode) control.setAttribute('aria-current', 'page');
          else control.removeAttribute('aria-current');
        }
        if (pushHistory) history.pushState({}, '', url);
      } catch (error) {
        if (error.name !== 'AbortError' && number === requestNumber) {
          if (picker) picker.value = links.find(item => item.getAttribute('aria-current') === 'page')?.getAttribute('href') || '';
          syntax.value = codeMain.dataset.language;
          notify('Could not load this file. Open the file link again to retry.');
        }
      } finally {
        if (number === requestNumber) {
          pane.removeAttribute('aria-busy');
          document.getElementById('copy-contents').disabled = false;
        }
      }
    }
    links.forEach(link => link.addEventListener('click', event => {
      if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      openFile(link);
    }));
    picker?.addEventListener('change', () => {
      const link = links.find(item => item.getAttribute('href') === picker.value);
      if (link) openFile(link);
    });
    for (const [control, mode] of [[viewCode, 'code'], [viewPreview, 'preview']]) {
      control.addEventListener('click', event => {
        if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        const link = links.find(item => item.getAttribute('aria-current') === 'page');
        openFile(link, true, syntax.value, mode);
      });
    }
    syntax.addEventListener('change', () => syntaxForm.requestSubmit());
    syntaxForm.addEventListener('submit', event => {
      event.preventDefault();
      const link = links.find(item => item.getAttribute('aria-current') === 'page');
      openFile(link, true, syntax.value);
    });
    window.addEventListener('popstate', () => {
      const params = new URL(location.href).searchParams;
      const id = params.get('file_id') || codeMain.dataset.initialFile;
      const link = links.find(item => item.dataset.fileId === id);
      if (link || !tree) openFile(link, false, params.get('language') || '', params.get('view') || 'code');
    });
    if (!tree) return;

    function focusItem(item) {
      if (!item) return;
      tree.querySelectorAll('[role="treeitem"]').forEach(node => { node.tabIndex = node === item ? 0 : -1; });
      item.focus();
    }
    tree.addEventListener('focusin', event => {
      if (event.target.matches('[role="treeitem"]')) {
        tree.querySelectorAll('[role="treeitem"]').forEach(node => { node.tabIndex = node === event.target ? 0 : -1; });
      }
    });
    tree.querySelectorAll('details').forEach(folder => folder.addEventListener('toggle', () => {
      folder.querySelector('summary').setAttribute('aria-expanded', String(folder.open));
      const focused = folder.querySelector('[tabindex="0"]');
      if (!folder.open && focused && focused !== folder.querySelector('summary')) focusItem(folder.querySelector('summary'));
    }));
    tree.addEventListener('keydown', event => {
      const current = event.target.closest('[role="treeitem"]');
      if (!current) return;
      const visible = [...tree.querySelectorAll('[role="treeitem"]')].filter(item => item.getClientRects().length);
      const index = visible.indexOf(current);
      const isFolder = current.tagName === 'SUMMARY';
      if (event.key === 'ArrowDown') focusItem(visible[Math.min(index + 1, visible.length - 1)]);
      else if (event.key === 'ArrowUp') focusItem(visible[Math.max(index - 1, 0)]);
      else if (event.key === 'Home') focusItem(visible[0]);
      else if (event.key === 'End') focusItem(visible[visible.length - 1]);
      else if (event.key === 'ArrowRight' && isFolder) {
        if (!current.parentElement.open) current.parentElement.open = true;
        else focusItem(visible[index + 1]);
      } else if (event.key === 'ArrowLeft') {
        if (isFolder && current.parentElement.open) current.parentElement.open = false;
        else focusItem(current.parentElement.parentElement.closest('details')?.querySelector('summary'));
      } else if (event.key === ' ' && !isFolder) current.click();
      else return;
      event.preventDefault();
    });
  }
})();
