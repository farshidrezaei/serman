/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,vue}"],
  theme: {
    extend: {
      colors: {
        ink: "#0b0d0f",
        graphite: "#14171a",
        steel: "#1f2428",
        fog: "#9aa3ad",
        neon: "#00ff9c"
      },
      boxShadow: {
        neon: "0 0 0 1px rgba(0,255,156,0.25), 0 0 30px rgba(0,255,156,0.18)"
      }
    }
  },
  plugins: []
};
