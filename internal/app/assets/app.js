(() => {
  document.documentElement.classList.add('js');
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
  document.getElementById('wrap-lines')?.addEventListener('click', event => {
    const wrapped = pane.classList.toggle('wrap');
    event.currentTarget.setAttribute('aria-pressed', String(wrapped));
  });

  const tree = document.getElementById('file-tree');
  if (tree) {
    const links = [...tree.querySelectorAll('.file-link')];
    const picker = document.getElementById('file-picker');
    let controller;
    let requestNumber = 0;
    async function openFile(link, pushHistory = true) {
      controller?.abort();
      controller = new AbortController();
      const number = ++requestNumber;
      pane.setAttribute('aria-busy', 'true');
      document.getElementById('copy-contents').disabled = true;
      try {
        const response = await fetch(link.href + '&partial=1', { credentials: 'same-origin', signal: controller.signal });
        if (response.status === 401) { location.assign(link.href); return; }
        if (!response.ok) throw new Error('File unavailable');
        const code = await response.text();
        if (number !== requestNumber) return;
        pane.innerHTML = code;
        pane.scrollTop = 0;
        pane.scrollLeft = 0;
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
        if (pushHistory) history.pushState({}, '', link.href);
      } catch (error) {
        if (error.name !== 'AbortError' && number === requestNumber) {
          picker.value = links.find(item => item.getAttribute('aria-current') === 'page').getAttribute('href');
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
    picker.addEventListener('change', () => {
      const link = links.find(item => item.getAttribute('href') === picker.value);
      if (link) openFile(link);
    });
    window.addEventListener('popstate', () => {
      const id = new URL(location.href).searchParams.get('file_id');
      const link = id ? links.find(item => item.dataset.fileId === id) : links.find(item => item.dataset.fileId === document.querySelector('.code-main').dataset.initialFile);
      if (link) openFile(link, false);
    });
    document.querySelector('.code-main').dataset.initialFile = links.find(item => item.getAttribute('aria-current') === 'page').dataset.fileId;

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
