// Kept framework-light: Alpine owns the batch list, HTMX owns requests,
// and a native dialog provides keyboard focus trapping and Escape handling.
window.magnetBatch = function () {
    return {
        draft: '', links: [], error: '', busy: false,
        label(link) {
            try {
                const params = new URL(link).searchParams;
                return params.get('dn') || params.get('xt') || 'Magnet link';
            } catch (_) { return 'Magnet link'; }
        },
        add() {
            const link = this.draft.trim();
            let valid = false;
            try { valid = /^magnet:\?/i.test(link) && !/[\r\n]/.test(link) && !!new URL(link).searchParams.get('xt'); } catch (_) {}
            if (!valid) {
                this.error = 'Paste one valid magnet link.'; return;
            }
            if (this.links.includes(link)) { this.error = 'That link is already added.'; return; }
            if (this.links.length >= 50) { this.error = 'You can add up to 50 links.'; return; }
            if (link.length > 8192 || this.links.join('\n').length + link.length > 110000) {
                this.error = 'These links are too large for one import.'; return;
            }
            this.links.push(link); this.draft = ''; this.error = '';
            this.$nextTick(() => this.$refs.draft.focus());
        },
        imported(accepted) {
            this.links = this.links.filter(link => !accepted.includes(link));
        }
    };
};

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
