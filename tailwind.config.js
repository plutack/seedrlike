export default {
  darkMode: "selector",
  content: [
    "./views/**/*.{html,templ,go}",
  ],
  theme: {
    extend: {},
  },
  plugins: [require("@tailwindcss/typography"), require("daisyui")],
};
