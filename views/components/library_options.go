package components

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const MaxLibraryQueryLength = 200

type LibraryOptions struct {
	Query string
	Sort  string
	Order string
}

type libraryOptionsKey struct{}

// ParseLibraryOptions validates user input before it reaches SQL or templates.
func ParseLibraryOptions(values url.Values) (LibraryOptions, error) {
	o := LibraryOptions{Query: strings.TrimSpace(values.Get("q")), Sort: values.Get("sort"), Order: values.Get("order")}
	if utf8.RuneCountInString(o.Query) > MaxLibraryQueryLength {
		return o, fmt.Errorf("library search must be at most %d characters", MaxLibraryQueryLength)
	}
	switch o.Sort {
	case "name", "size", "date":
	default:
		o.Sort = "date"
	}
	if o.Order != "asc" && o.Order != "desc" {
		if o.Sort == "name" {
			o.Order = "asc"
		} else {
			o.Order = "desc"
		}
	}
	return o, nil
}

func WithLibraryOptions(ctx context.Context, options LibraryOptions) context.Context {
	return context.WithValue(ctx, libraryOptionsKey{}, options)
}

func LibraryOptionsFromContext(ctx context.Context) LibraryOptions {
	if options, ok := ctx.Value(libraryOptionsKey{}).(LibraryOptions); ok {
		return options
	}
	options, _ := ParseLibraryOptions(nil)
	return options
}

func (o LibraryOptions) Values() url.Values {
	v := url.Values{"sort": {o.Sort}, "order": {o.Order}}
	if o.Query != "" {
		v.Set("q", o.Query)
	}
	return v
}

func (o LibraryOptions) FolderURL(folderID string) string {
	return "/contents/" + url.PathEscape(folderID) + "?" + o.Values().Encode()
}

func (o LibraryOptions) DeleteURL(contentID, contentType, folderID string) string {
	v := o.Values()
	v.Set("type", contentType)
	v.Set("folder", folderID)
	return "/contents/" + url.PathEscape(contentID) + "?" + v.Encode()
}

func (o LibraryOptions) StaleDeleteURL(folderID string) string {
	v := o.Values()
	v.Set("folder", folderID)
	return "/contents?" + v.Encode()
}

func (o LibraryOptions) SortURL(folderID, sort string) string {
	if o.Sort == sort && o.Order == "asc" {
		o.Order = "desc"
	} else {
		o.Order = "asc"
	}
	o.Sort = sort
	return o.FolderURL(folderID)
}

func (o LibraryOptions) ClearURL(folderID string) string {
	o.Query = ""
	return o.FolderURL(folderID)
}
