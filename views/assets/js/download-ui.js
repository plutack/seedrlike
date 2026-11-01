// File selection is local until explicit submission; HTMX owns the upload.
window.torrentUpload = function () {
    return {
        file: null, error: '', busy: false, dragging: false, percent: 0, previewing: false,
        size(bytes) { return bytes < 1024 ? bytes + ' B' : bytes < 1048576 ? (bytes / 1024).toFixed(1) + ' KB' : (bytes / 1048576).toFixed(1) + ' MB'; },
        clearFeedback() { this.$root.querySelector('#torrent-upload-response').replaceChildren(); },
        choose(files) {
            if (this.busy) return;
            this.error = ''; this.clearFeedback();
            if (!files || !files.length) return;
            const file = files[0];
            if (files.length !== 1 || !/\.torrent$/i.test(file.name) || !file.size || file.size > 4 * 1024 * 1024) {
                this.file = null; this.$refs.file.value = '';
                this.error = 'Choose one non-empty .torrent file up to 4 MB.'; return;
            }
            this.file = file; this.percent = 0;
        },
        drop(event) {
            this.dragging = false;
            if (this.busy) return;
            const files = event.dataTransfer.files;
            this.choose(files);
            if (this.file) {
                const transfer = new DataTransfer(); transfer.items.add(this.file);
                this.$refs.file.files = transfer.files;
            }
        },
        remove() {
            if (this.busy) return;
            this.file = null; this.$refs.file.value = ''; this.error = ''; this.percent = 0;
            this.clearFeedback(); this.$refs.file.focus();
        },
        start(event) {
            this.previewing = event.detail?.target?.id === 'file-picker-content';
            if (!this.file || this.busy) { event.preventDefault(); return; }
            this.busy = true; this.percent = 0; this.error = ''; this.clearFeedback();
        },
        progress(event) {
            if (this.busy && event.detail.total > 0) this.percent = Math.min(100, Math.round(event.detail.loaded / event.detail.total * 100));
        },
        finish(event) {
            this.busy = false; this.previewing = false;
            if (event.detail.failed) this.error = 'Upload failed. Your file is still selected. Try again.';
        },
        accepted() {
            this.file = null; this.$refs.file.value = ''; this.error = ''; this.percent = 0;
            // Keep the user's ZIP preference for the next upload.
        }
    };
};

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
        // Multipart parameters can expose action as a FormData value/list rather
        // than a scalar. The resolved HTMX target is the stable preview contract.
        if (detail.target?.id !== 'file-picker-content') return;
        const modal = dialog();
        if (!modal) return;
        if (previewXHR) previewXHR.abort();
        previewXHR = detail.xhr;
        const template = document.getElementById('file-picker-loading');
        content().replaceChildren(template.content.cloneNode(true));
        if (!modal.open) modal.showModal();
    });
    // Keep the file browser-local: copy it to the selection form only after
    // preview. No temporary server upload or client-editable metadata token.
    document.addEventListener('htmx:afterSwap', event => {
        if (event.detail.target.id !== 'file-picker-content') return;
        const input = document.getElementById('torrent-selection-file');
        const original = document.getElementById('torrent-file');
        if (input && original && original.files.length) {
            const transfer = new DataTransfer(); transfer.items.add(original.files[0]);
            input.files = transfer.files;
        }
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
