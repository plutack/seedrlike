package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTorrentUploadReplacesBatchImportAndRetainsModalPreview(t *testing.T) {
	var out bytes.Buffer
	if err := Download().Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if strings.Index(html, `id="download-response"`) > strings.Index(html, `aria-labelledby="torrent-upload-title"`) {
		t.Error("search results must appear before the torrent upload panel")
	}
	for _, want := range []string{`name="torrent-file"`, `type="file"`, `hx-post="/downloads/torrent"`, `hx-encoding="multipart/form-data"`, `torrent-uploaded.window`, "Add to queue", `hx-trigger="input changed delay:500ms, search-now"`, `x-on:keydown.enter`, `hx-disabled-elt="closest form"`, `x-data="torrentUpload()"`, `x-on:drop.prevent`, `aria-label="Torrent file upload progress"`, `id="torrent-file-error"`, `x-bind:aria-busy="busy"`, `hx-target="#file-picker-content"`, `id="file-picker"`, `aria-labelledby="file-picker-title"`, `id="file-picker-loading"`, `loading-spinner`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing batch/modal behavior %s", want)
		}
	}
	for _, old := range []string{"Import multiple magnets", "magnetBatch", "/downloads/batch", "One magnet link per line", `id="metadata-loading"`, "max-w-2xl"} {
		if strings.Contains(html, old) {
			t.Errorf("obsolete UI remains: %s", old)
		}
	}
}

func TestQueueFeedbackDismissalAndExpiry(t *testing.T) {
	for _, failed := range []bool{false, true} {
		var out bytes.Buffer
		if err := DownloadFeedback("<script>unsafe</script>", failed).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		for _, want := range []string{`aria-label="Dismiss notification"`, "setTimeout", "6000", "w-full min-w-0", "&lt;script&gt;"} {
			if !strings.Contains(html, want) {
				t.Errorf("missing feedback behavior %s", want)
			}
		}
		if strings.Contains(html, "<script>unsafe</script>") {
			t.Error("feedback is not escaped")
		}
	}
}

func TestFilePickerRetainsExplicitSelectionAndScroll(t *testing.T) {
	var out bytes.Buffer
	if err := FileSelection("magnet:?xt=test", "Example", true, []SelectableFile{{Index: 3, Name: "a.txt", Size: 10}}).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{`name="selection" value="explicit"`, `name="files" value="3"`, `hx-target="#file-picker-feedback"`, "max-h-96 overflow-y-auto", "Download selected"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing file selection behavior %s", want)
		}
	}
}
