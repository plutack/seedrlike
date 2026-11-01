// Native dialog coordinates HTMX metadata loading and cancellation.
(function () {
    let previewXHR = null;
    function dialog() { return document.getElementById('file-picker'); }
    function content() { return document.getElementById('file-picker-content'); }
    function error(message) {
        const target = content();
        if (!target) return;
        target.replaceChildren();
        const text = document.createElement('p');
        text.className = 'py-6 text-sm text-error';
        text.setAttribute('role', 'alert');
        text.textContent = message;
        target.appendChild(text);
    }
    document.addEventListener('htmx:beforeRequest', event => {
        const detail = event.detail;
        if (!detail.requestConfig || detail.requestConfig.path !== '/downloads/preview') return;
        const modal = dialog();
        if (!modal) return;
        if (previewXHR) previewXHR.abort();
        previewXHR = detail.xhr;
        const template = document.getElementById('file-picker-loading');
        content().replaceChildren(template.content.cloneNode(true));
        if (!modal.open) modal.showModal();
    });
    document.addEventListener('htmx:beforeSwap', event => {
        if (event.detail.target.id === 'file-picker-content' && !dialog().open) {
            event.detail.shouldSwap = false;
        }
    });
    document.addEventListener('htmx:afterRequest', event => {
        if (event.detail.xhr !== previewXHR) return;
        previewXHR = null;
        if (dialog().open && event.detail.failed) {
            error('Could not load files. Close this window and try again.');
        }
    });
    document.addEventListener('download-queued', () => {
        const modal = dialog();
        if (modal && modal.open) modal.close();
    });
    // close does not bubble; capture also catches Escape and backdrop dismissal.
    document.addEventListener('close', event => {
        if (event.target.id !== 'file-picker') return;
        const xhr = previewXHR;
        previewXHR = null;
        if (xhr) xhr.abort();
    }, true);
})();
