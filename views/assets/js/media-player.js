(function () {
  'use strict';
  let request = null;
  let generation = 0;
  let active = null;
  let opener = null;

  function stop() {
    generation++;
    if (request) request.abort();
    request = null;
    if (active) {
      active.pause();
      active.removeAttribute('src');
      active.load();
      active = null;
    }
    const content = document.getElementById('media-player-content');
    if (content) content.replaceChildren();
  }

  async function open(button) {
    const dialog = document.getElementById('media-player');
    if (!dialog) return;
    stop();
    opener = button;
    const version = generation;
    const status = document.getElementById('media-player-status');
    const error = document.getElementById('media-player-error');
    const download = document.getElementById('media-player-download');
    document.getElementById('media-player-title').textContent = 'Media player';
    status.hidden = false;
    status.textContent = 'Loading media…';
    error.hidden = true;
    download.hidden = true;
    download.removeAttribute('href');
    if (!dialog.open) dialog.showModal();
    request = new AbortController();
    try {
      const response = await fetch(button.dataset.mediaUrl, { signal: request.signal, headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(response.status === 404 ? 'This file is no longer available or you do not have access.' : 'Could not load this media file. Close the player and try again.');
      const data = await response.json();
      if (version !== generation || !dialog.open) return;
      if (!['video', 'audio'].includes(data.kind) || !data.streamURL.startsWith('/media/')) throw new Error('This media format is not supported.');
      document.getElementById('media-player-title').textContent = data.name;
      download.href = data.downloadURL;
      download.hidden = false;
      const player = document.createElement(data.kind);
      active = player;
      player.controls = true;
      player.preload = 'metadata';
      player.setAttribute('aria-label', data.name);
      player.style.width = '100%';
      if (data.kind === 'video') {
        player.playsInline = true;
        player.style.maxHeight = '65dvh';
      }
      player.addEventListener('loadedmetadata', function () {
        if (version !== generation) return;
        status.hidden = true;
      });
      player.addEventListener('waiting', function () {
        if (version !== generation) return;
        status.textContent = 'Buffering…';
        status.hidden = false;
      });
      player.addEventListener('playing', function () {
        if (version === generation) status.hidden = true;
      });
      player.addEventListener('error', function () {
        if (version !== generation) return;
        status.hidden = true;
        error.textContent = 'Your browser could not play this file, or the remote file is unavailable. Try downloading it instead.';
        error.hidden = false;
      });
      document.getElementById('media-player-content').replaceChildren(player);
      status.textContent = 'Loading preview. Use the player controls to start playback.';
      player.src = data.streamURL;
    } catch (err) {
      if (version !== generation || err.name === 'AbortError') return;
      status.hidden = true;
      error.textContent = err.message || 'Could not load this media file. Please try again.';
      error.hidden = false;
    }
  }

  document.addEventListener('click', function (event) {
    const button = event.target.closest('[data-media-url]');
    if (button) { event.preventDefault(); open(button); return; }
    if (event.target.closest('[data-media-close]')) document.getElementById('media-player').close();
  });
  document.addEventListener('close', function (event) {
    if (event.target.id !== 'media-player') return;
    stop();
    if (opener && opener.isConnected) opener.focus();
    opener = null;
  }, true);
  // HTMX navigation can replace the whole body. Release network and audio first.
  document.addEventListener('htmx:beforeSwap', function (event) {
    const target = event.detail.target;
    const dialog = document.getElementById('media-player');
    if (dialog && target && target.contains(dialog)) stop();
  });
})();
