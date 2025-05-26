package generate

//go:generate go tool templ generate
//go:generate sqlc generate

//go:generate tailwindcss-extra -i ./views/assets/css/input.css -o ./views/assets/css/tailwind.css --minify
