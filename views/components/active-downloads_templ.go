// Code generated by templ - DO NOT EDIT.

// templ: version: v0.3.819
package components

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

func ActiveDownloads() templ.Component {
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
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 1, "<div class=\"w-full max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-6\"><h2 class=\"text-2xl sm:text-3xl font-bold mb-6\">Active Downloads</h2><div x-data=\"{ \n                torrents: {},\n                removeTorrent(id) {\n                    delete this.torrents[id];\n                }\n            }\" x-init=\"\n                let protocol = window.location.protocol === &#39;https:&#39; ? &#39;wss&#39; : &#39;ws&#39;;\n                let ws = new WebSocket(`${protocol}://${window.location.host}/ws`);\n    \n                ws.onmessage = (event) =&gt; {\n                    let data = JSON.parse(event.data);\n            \n                    if (data.type === &#39;torrent update&#39;){\n                        torrents[data.id] = data;\n                        if (data.status === &#39;completed&#39;) {\n                            setTimeout(() =&gt; removeTorrent(data.id), 2000);\n                        }\n                        return;\n                    }\n            \n                    if(data.type === &#39;upload refresh&#39;){\n                        htmx.trigger(&#39;#download-list&#39;, &#39;refresh&#39;);\n                        return;\n                    }\n                };\n                ws.onclose = () =&gt; {\n                    torrents = {};\n                };\n            \" class=\"space-y-4\"><template x-for=\"[id, torrent] in Object.entries(torrents)\" :key=\"id\"><div class=\"card bg-base-200 shadow-xl transition-all duration-200 hover:shadow-2xl\"><div class=\"card bg-base-200 shadow-xl transition-all duration-200 hover:shadow-2xl\"><div class=\"card-body p-4 sm:p-6\"><div class=\"flex flex-col sm:flex-row sm:items-center justify-between gap-2 sm:gap-4\"><h3 class=\"card-title text-base sm:text-lg break-all\" x-text=\"torrent.name\"></h3><div class=\"badge whitespace-nowrap text-sm\" x-text=\"torrent.status\" x-bind:class=\"{\n                                    &#39;badge-neutral&#39;: torrent.status === &#39;pending&#39;,\n                                    &#39;badge-primary&#39;: torrent.status === &#39;downloading&#39;,\n                                    &#39;badge-success&#39;: torrent.status === &#39;completed&#39;\n                                }\"></div></div><div x-show=\"torrent.status === &#39;downloading&#39;\" class=\"mt-4\"><div class=\"flex items-center gap-3 sm:gap-4\"><progress class=\"progress progress-primary w-full\" x-bind:value=\"torrent.progress\" max=\"100\"></progress> <span class=\"text-sm font-medium min-w-[4rem] text-right\" x-text=\"torrent.progress + &#39;%&#39;\"></span></div><div class=\"mt-3 flex flex-col sm:flex-row justify-between text-sm text-base-content/70\"><span class=\"whitespace-nowrap\" x-text=\"&#39;Speed: &#39; + (torrent.speed || &#39;0 KB/s&#39;)\"></span> <span class=\"whitespace-nowrap\" x-text=\"&#39;ETA: &#39; + (torrent.eta || &#39;calculating...&#39;)\"></span></div></div></div></div></div></template><div x-show=\"Object.keys(torrents).length === 0\" class=\"text-center p-6 sm:p-8 text-base-content/70 bg-base-200/50 rounded-lg\">No active downloads</div></div></div>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return nil
	})
}

var _ = templruntime.GeneratedTemplate
