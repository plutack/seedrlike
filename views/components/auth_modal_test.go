package components

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestAuthModalSubmissionControls(t *testing.T) {
	for _, showLogin := range []bool{true, false} {
		t.Run(fmt.Sprint(showLogin), func(t *testing.T) {
			var out bytes.Buffer
			if err := AuthModal(showLogin).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(out.String()))
			if err != nil {
				t.Fatal(err)
			}
			var forms, enterInputs, remember, tabs, cloaked int
			labels := map[string]bool{}
			inputs := map[string]bool{}
			var walk func(*html.Node)
			walk = func(n *html.Node) {
				attrs := map[string]string{}
				for _, a := range n.Attr {
					attrs[a.Key] = a.Val
				}
				if n.Type == html.ElementNode {
					switch n.Data {
					case "form":
						forms++
						if attrs["method"] != "post" || (attrs["action"] != "/login" && attrs["action"] != "/register") || attrs["hx-post"] != attrs["action"] || attrs["hx-trigger"] != "submit" || attrs["hx-swap"] != "none" {
							t.Fatalf("invalid auth form: %+v", attrs)
						}
					case "button":
						if attrs["type"] == "" {
							t.Fatal("implicit submit button")
						}
						if attrs["@click"] != "" {
							tabs++
							if attrs["type"] != "button" {
								t.Fatal("tab submits")
							}
						}
						if attrs["onclick"] != "" && attrs["type"] != "button" {
							t.Fatal("close submits")
						}
					case "label":
						labels[attrs["for"]] = true
					case "input":
						inputs[attrs["id"]] = true
						if attrs["type"] == "checkbox" {
							remember++
							if attrs["name"] != "remember_me" || attrs["value"] != "on" {
								t.Fatal("invalid remember checkbox")
							}
							if _, ok := attrs["checked"]; ok {
								t.Fatal("remember-me must default unchecked")
							}
							if attrs["@keydown.enter.prevent"] != "" {
								t.Fatal("checkbox Enter must not submit")
							}
						} else {
							enterInputs++
							if attrs["@keydown.enter.prevent"] != "$el.form.requestSubmit()" {
								t.Fatal("missing scoped Enter handler")
							}
							want := "username"
							if attrs["id"] == "login-password" {
								want = "current-password"
							}
							if attrs["id"] == "register-password" {
								want = "new-password"
							}
							if attrs["autocomplete"] != want {
								t.Fatalf("autocomplete mismatch: %+v", attrs)
							}
						}
					}
					if _, ok := attrs["x-cloak"]; ok {
						cloaked++
					}
					if strings.Contains(attrs["x-data"], "isLogin:") && attrs["x-data"] != fmt.Sprintf("{ isLogin: %t }", showLogin) {
						t.Fatal("wrong initial tab")
					}
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			walk(doc)
			if forms != 2 || enterInputs != 4 || remember != 1 || tabs != 2 || cloaked != 2 {
				t.Fatalf("unexpected controls: forms=%d inputs=%d remember=%d tabs=%d cloaked=%d", forms, enterInputs, remember, tabs, cloaked)
			}
			for id := range inputs {
				if id == "" || !labels[id] {
					t.Fatalf("missing input label for %q", id)
				}
			}
			if strings.Contains(out.String(), `method="dialog"`) {
				t.Fatal("dialog forms intercept Enter")
			}
		})
	}
}
