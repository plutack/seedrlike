// Package media describes browser-playable containers and constructs safe Gofile URLs.
// Container support does not guarantee that a browser supports the codecs inside it.
package media

import (
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
)

// Type is a browser media element kind and a deterministic container MIME type.
type Type struct {
	Kind string
	MIME string
}

// Detect deliberately uses an extension allowlist, not untrusted stored MIME data.
func Detect(name string) (Type, bool) {
	var t Type
	switch strings.ToLower(path.Ext(name)) {
	case ".mp4", ".m4v":
		t = Type{"video", "video/mp4"}
	case ".webm":
		t = Type{"video", "video/webm"}
	case ".ogv":
		t = Type{"video", "video/ogg"}
	case ".mp3":
		t = Type{"audio", "audio/mpeg"}
	case ".m4a":
		t = Type{"audio", "audio/mp4"}
	case ".aac":
		t = Type{"audio", "audio/aac"}
	case ".ogg", ".oga", ".opus":
		t = Type{"audio", "audio/ogg"}
	case ".wav":
		t = Type{"audio", "audio/wav"}
	case ".flac":
		t = Type{"audio", "audio/flac"}
	default:
		return Type{}, false
	}
	return t, true
}

var serverLabel = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
var contentID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

// DownloadURL accepts only a single DNS server label and an opaque content ID.
// The filename is one escaped URL segment, never a path or a query string.
func DownloadURL(server, id, name string) (string, error) {
	if !serverLabel.MatchString(server) || !contentID.MatchString(id) {
		return "", errors.New("invalid Gofile server or content ID")
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", errors.New("invalid Gofile filename")
	}
	return "https://" + strings.ToLower(server) + ".gofile.io/download/web/" + url.PathEscape(id) + "/" + url.PathEscape(name), nil
}

// SafeDownloadURL validates redirect targets. Gofile also serves downloads from
// gofileusercontent.com; a suffix match always includes the DNS label boundary.
func SafeDownloadURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"gofile.io", "gofileusercontent.com"} {
		if host == domain {
			return true
		}
		if strings.HasSuffix(host, "."+domain) {
			labels := strings.Split(strings.TrimSuffix(host, "."+domain), ".")
			for _, label := range labels {
				if !serverLabel.MatchString(label) {
					return false
				}
			}
			return true
		}
	}
	return false
}
