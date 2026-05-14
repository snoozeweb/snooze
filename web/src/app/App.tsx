import type { ReactNode } from "react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { Code, CodeBlock } from "@/shared/ui/Code";
import { EmptyState } from "@/shared/ui/EmptyState";
import { IconButton } from "@/shared/ui/IconButton";
import { Kbd } from "@/shared/ui/Kbd";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Spinner } from "@/shared/ui/Spinner";
import { useTheme } from "@/shared/hooks/useTheme";

export function App() {
  const { theme, toggleTheme } = useTheme();

  return (
    <main
      style={{
        maxWidth: 960,
        margin: "0 auto",
        padding: "var(--space-6) var(--space-5)",
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-6)",
      }}
    >
      <header
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <h1>Snooze · Primitives</h1>
        <Button
          variant="ghost"
          leadingIcon={theme === "dark" ? "sun" : "moon"}
          onClick={toggleTheme}
        >
          {theme === "dark" ? "Light" : "Dark"}
        </Button>
      </header>

      <Section title="Button">
        <Row>
          <Button variant="primary">Primary</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="danger">Danger</Button>
        </Row>
        <Row>
          <Button size="sm">Small</Button>
          <Button size="md">Medium</Button>
          <Button size="lg">Large</Button>
        </Row>
        <Row>
          <Button leadingIcon="plus">Add</Button>
          <Button trailingIcon="chevron-down">Filter</Button>
          <Button loading>Saving</Button>
          <Button disabled>Disabled</Button>
        </Row>
      </Section>

      <Section title="IconButton">
        <Row>
          <IconButton icon="refresh" label="Refresh" />
          <IconButton icon="search" label="Search" />
          <IconButton icon="trash" label="Delete" variant="danger" />
          <IconButton icon="plus" label="Add" variant="primary" />
          <IconButton icon="x" label="Close" size="lg" />
        </Row>
      </Section>

      <Section title="Badge">
        <Row>
          <Badge>neutral</Badge>
          <Badge variant="muted">muted</Badge>
          <Badge variant="info">info</Badge>
          <Badge variant="ok">ok</Badge>
          <Badge variant="warning">warning</Badge>
          <Badge variant="error">error</Badge>
          <Badge variant="critical">critical</Badge>
        </Row>
      </Section>

      <Section title="Spinner / Skeleton">
        <Row>
          <Spinner size={12} />
          <Spinner size={16} />
          <Spinner size={20} />
        </Row>
        <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-2)" }}>
          <Skeleton height={16} />
          <Skeleton width="60%" height={16} />
          <Skeleton width={120} height={20} radius="pill" />
        </div>
      </Section>

      <Section title="Card">
        <Card padded>
          <h2 style={{ margin: 0 }}>A card</h2>
          <p style={{ margin: 0, color: "var(--text-muted)" }}>With padding and a 1-px border.</p>
        </Card>
        <Card padded elevated>
          <h2 style={{ margin: 0 }}>Elevated card</h2>
          <p style={{ margin: 0, color: "var(--text-muted)" }}>Same shape, with a shadow.</p>
        </Card>
      </Section>

      <Section title="Kbd">
        <Row>
          <span>
            Press <Kbd>⌘</Kbd> + <Kbd>K</Kbd> to open the palette.
          </span>
          <span>
            Or <Kbd>Esc</Kbd> to close.
          </span>
        </Row>
      </Section>

      <Section title="Code / CodeBlock">
        <p>
          Inline: <Code>GET /api/v1/alert</Code> returns the list.
        </p>
        <CodeBlock copyable>
          {`{"type":"AND","args":[{"type":"EQUALS","field":"host","value":"srv-1"}]}`}
        </CodeBlock>
      </Section>

      <Section title="EmptyState">
        <Card padded>
          <EmptyState
            icon="bell-off"
            title="No alerts in this view"
            description="Try widening your time window or clearing filters."
            action={
              <Button variant="primary" leadingIcon="refresh">
                Refresh
              </Button>
            }
          />
        </Card>
      </Section>
    </main>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section style={{ display: "flex", flexDirection: "column", gap: "var(--space-3)" }}>
      <h2
        style={{
          fontSize: "var(--text-md)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: "var(--text-muted)",
        }}
      >
        {title}
      </h2>
      {children}
    </section>
  );
}

function Row({ children }: { children: ReactNode }) {
  return (
    <div
      style={{
        display: "flex",
        flexWrap: "wrap",
        alignItems: "center",
        gap: "var(--space-3)",
      }}
    >
      {children}
    </div>
  );
}
