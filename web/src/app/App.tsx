import { useTheme } from "@/shared/hooks/useTheme";

export function App() {
  const { theme, toggleTheme } = useTheme();
  const next = theme === "dark" ? "light" : "dark";

  return (
    <main
      style={{
        display: "flex",
        flexDirection: "column",
        justifyContent: "center",
        alignItems: "center",
        height: "100%",
        gap: "var(--space-2)",
        padding: "var(--space-5)",
      }}
    >
      <h1 style={{ color: "var(--accent)" }}>Snooze</h1>
      <p style={{ color: "var(--text-muted)" }}>
        Design foundation is up. Tokens, themes, and the toggle work.
      </p>
      <button
        type="button"
        onClick={toggleTheme}
        style={{
          marginTop: "var(--space-3)",
          padding: "var(--space-1) var(--space-3)",
          background: "var(--bg-surface)",
          color: "var(--text-strong)",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius-md)",
          cursor: "pointer",
          fontSize: "var(--text-sm)",
        }}
      >
        Switch to {next}
      </button>
    </main>
  );
}
