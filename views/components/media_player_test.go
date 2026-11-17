package components

import (
	"bytes"
	"context"
	database "github.com/plutack/seedrlike/internal/database/sqlc"
	"strings"
	"testing"
)

func TestLibraryPlaybackOnlyForSupportedFiles(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		playable   bool
	}{
		{"movie.MP4", "file", true}, {"song.mp3", "file", true}, {"movie.webm", "file", true},
		{"movie.mkv", "file", false}, {"archive.zip", "file", false}, {"folder.mp4", "folder", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := DownloadList(false, []database.GetFolderContentsRow{{ID: "example", Name: tc.name, Type: tc.kind, Server: "store1"}}, "root", false).Render(context.Background(), &out)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(out.String(), `data-media-url="/media/example"`); got != tc.playable {
				t.Fatalf("play control = %v, want %v", got, tc.playable)
			}
			if !strings.Contains(out.String(), `aria-label="Download"`) {
				t.Fatal("missing download fallback")
			}
		})
	}
}

func TestMediaPlayerAccessibleShell(t *testing.T) {
	var out bytes.Buffer
	if err := MediaPlayer().Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="media-player"`, `aria-labelledby="media-player-title"`, `data-media-close`, `role="status"`, `role="alert"`, `hx-boost="false"`, `codec support`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s", want)
		}
	}
}
