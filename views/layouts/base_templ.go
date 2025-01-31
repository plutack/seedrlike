// Code generated by templ - DO NOT EDIT.

// templ: version: v0.3.819
package layouts

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

import (
	"github.com/plutack/seedrlike/internal/database/sqlc"
	"github.com/plutack/seedrlike/views/components"
)

const (
	defaultTitle = "seedrlike"
)

func Base(returnErr bool, torrents []database.GetFolderContentsRow, rootFolderID string) templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var1 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var1 == nil {
			templ_7745c5c3_Var1 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 1, "<!doctype html><html lang=\"en\" class=\"h-full\"><head><title>Seedrlike</title><link rel=\"icon\" type=\"image/png\" href=\"/assets/bird.png\" hx-preserve=\"true\"><meta charset=\"UTF-8\" hx-preserve=\"true\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\" hx-preserve=\"true\"><meta name=\"description\" content=\"A seedr.cc like application\"><script src=\"/assets/js/htmx.min.js\" hx-preserve=\"true\"></script><script src=\"/assets/js/alpine.min.js\" hx-preserve=\"true\"></script><link href=\"/assets/css/tailwind.css\" rel=\"stylesheet\" hx-preserve=\"true\"><script hx-preserve=\"true\">\n                if (localStorage.theme === 'dark' || (!('theme' in localStorage) && window.matchMedia('(prefers-color-scheme: dark)').matches)) {\n                    document.documentElement.classList.add('dark')\n                } else {\n                    document.documentElement.classList.remove('dark')\n                }\n                function toggleTheme() {\n                    let theme = localStorage.theme === 'dark' ? 'light' : 'dark'\n                    if (!('theme' in localStorage) && window.matchMedia('(prefers-color-scheme: dark)').matches) {\n                        theme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'light' : 'dark'\n                    }\n                    \n                    localStorage.theme = theme\n                    document.documentElement.classList.toggle('dark', theme === 'dark')\n                }\n                console.log(\"hello\")\n            </script></head><body class=\"min-h-screen flex flex-col antialiased\">")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = components.Header().Render(ctx, templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 2, "<main class=\"flex-grow\">")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = components.Download().Render(ctx, templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = components.DownloadList(returnErr, torrents, rootFolderID).Render(ctx, templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 3, "</main>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = components.Footer().Render(ctx, templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 4, "</body></html>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return nil
	})
}

var _ = templruntime.GeneratedTemplate
