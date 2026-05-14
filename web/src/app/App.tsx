import { useState } from "react";
import type { ReactNode } from "react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { Checkbox } from "@/shared/ui/Checkbox";
import { Code } from "@/shared/ui/Code";
import { Combobox } from "@/shared/ui/Combobox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
  DialogTrigger,
} from "@/shared/ui/Dialog";
import {
  Drawer,
  DrawerBody,
  DrawerContent,
  DrawerFooter,
  DrawerTitle,
  DrawerTrigger,
} from "@/shared/ui/Drawer";
import { EmptyState } from "@/shared/ui/EmptyState";
import { IconButton } from "@/shared/ui/IconButton";
import { Input } from "@/shared/ui/Input";
import { Kbd } from "@/shared/ui/Kbd";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "@/shared/ui/Menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/shared/ui/Popover";
import { RadioGroup, RadioOption } from "@/shared/ui/Radio";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Spinner } from "@/shared/ui/Spinner";
import { Switch } from "@/shared/ui/Switch";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { toast } from "@/shared/ui/toast/useToast";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { Textarea } from "@/shared/ui/Textarea";
import { Tooltip, TooltipProvider } from "@/shared/ui/Tooltip";
import { useTheme } from "@/shared/hooks/useTheme";

const fields = [
  { value: "host", label: "host" },
  { value: "message", label: "message" },
  { value: "severity", label: "severity" },
  { value: "environment", label: "environment" },
];

export function App() {
  const { theme, toggleTheme } = useTheme();
  const [switchOn, setSwitchOn] = useState(false);
  const [pickedField, setPickedField] = useState<string | undefined>(undefined);

  return (
    <TooltipProvider>
      <ToastProvider>
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
              <Button leadingIcon="plus">Add</Button>
              <Button trailingIcon="chevron-down">Filter</Button>
              <Button loading>Saving</Button>
              <Button disabled>Disabled</Button>
            </Row>
          </Section>

          <Section title="IconButton">
            <Row>
              <IconButton icon="refresh" label="Refresh" />
              <IconButton icon="trash" label="Delete" variant="danger" />
              <IconButton icon="plus" label="Add" variant="primary" />
            </Row>
          </Section>

          <Section title="Badge">
            <Row>
              <Badge>neutral</Badge>
              <Badge variant="info">info</Badge>
              <Badge variant="ok">ok</Badge>
              <Badge variant="warning">warning</Badge>
              <Badge variant="error">error</Badge>
              <Badge variant="critical">critical</Badge>
            </Row>
          </Section>

          <Section title="Tooltip">
            <Row>
              <Tooltip content="Refresh the list">
                <IconButton icon="refresh" label="Refresh" />
              </Tooltip>
              <Tooltip content="Permanent action" side="right">
                <Button variant="danger" leadingIcon="trash">
                  Delete
                </Button>
              </Tooltip>
            </Row>
          </Section>

          <Section title="Popover">
            <Row>
              <Popover>
                <PopoverTrigger>
                  <Button trailingIcon="chevron-down">Filters</Button>
                </PopoverTrigger>
                <PopoverContent>
                  <p style={{ margin: 0 }}>Filter panel content.</p>
                </PopoverContent>
              </Popover>
            </Row>
          </Section>

          <Section title="Menu">
            <Row>
              <Menu>
                <MenuTrigger>
                  <IconButton icon="more-horizontal" label="More" />
                </MenuTrigger>
                <MenuContent>
                  <MenuItem leadingIcon="edit" onSelect={() => undefined}>
                    Edit
                  </MenuItem>
                  <MenuItem leadingIcon="copy" shortcut="⌘D" onSelect={() => undefined}>
                    Duplicate
                  </MenuItem>
                  <MenuSeparator />
                  <MenuItem leadingIcon="trash" danger onSelect={() => undefined}>
                    Delete
                  </MenuItem>
                </MenuContent>
              </Menu>
            </Row>
          </Section>

          <Section title="Dialog">
            <Dialog>
              <DialogTrigger>
                <Button variant="danger">Delete record</Button>
              </DialogTrigger>
              <DialogContent>
                <DialogTitle>Delete record</DialogTitle>
                <DialogBody>
                  <DialogDescription>This cannot be undone.</DialogDescription>
                </DialogBody>
                <DialogFooter>
                  <Button>Cancel</Button>
                  <Button variant="danger">Delete</Button>
                </DialogFooter>
              </DialogContent>
            </Dialog>
          </Section>

          <Section title="Drawer">
            <Drawer>
              <DrawerTrigger>
                <Button>Edit rule</Button>
              </DrawerTrigger>
              <DrawerContent>
                <DrawerTitle>Edit rule</DrawerTitle>
                <DrawerBody>
                  <p style={{ margin: 0 }}>Editing surface lives here in feature work.</p>
                </DrawerBody>
                <DrawerFooter>
                  <Button>Cancel</Button>
                  <Button variant="primary">Save</Button>
                </DrawerFooter>
              </DrawerContent>
            </Drawer>
          </Section>

          <Section title="Tabs">
            <Tabs defaultValue="rules">
              <TabList>
                <TabTrigger value="rules">Rules</TabTrigger>
                <TabTrigger value="aggregates">Aggregates</TabTrigger>
              </TabList>
              <TabPanel value="rules">Rules go here.</TabPanel>
              <TabPanel value="aggregates">Aggregates go here.</TabPanel>
            </Tabs>
          </Section>

          <Section title="Toast">
            <Row>
              <Button onClick={() => toast.success("Saved successfully")}>Toast success</Button>
              <Button
                variant="danger"
                onClick={() => toast.error("Couldn't save", { traceId: "abc123" })}
              >
                Toast error
              </Button>
              <Button variant="secondary" onClick={() => toast.info("Heads up.")}>
                Toast info
              </Button>
            </Row>
          </Section>

          <Section title="Input + Textarea">
            <Row>
              <Input placeholder="Search…" leadingIcon="search" />
              <Input placeholder="Invalid" invalid defaultValue="oops" />
            </Row>
            <Textarea placeholder="Type a long comment…" rows={3} />
          </Section>

          <Section title="Switch / Checkbox / Radio">
            <Row>
              <Switch checked={switchOn} onCheckedChange={setSwitchOn} aria-label="Auto-refresh" />
              <Checkbox aria-label="Include closed" defaultChecked />
              <RadioGroup defaultValue="a">
                <RadioOption value="a" aria-label="A" />
                <RadioOption value="b" aria-label="B" />
                <RadioOption value="c" aria-label="C" />
              </RadioGroup>
            </Row>
          </Section>

          <Section title="Select">
            <Row>
              <Select>
                <SelectTrigger placeholder="Pick a severity…" />
                <SelectContent>
                  <SelectItem value="info">info</SelectItem>
                  <SelectItem value="warning">warning</SelectItem>
                  <SelectItem value="error">error</SelectItem>
                  <SelectItem value="critical">critical</SelectItem>
                </SelectContent>
              </Select>
            </Row>
          </Section>

          <Section title="Combobox">
            <Row>
              <Combobox
                options={fields}
                {...(pickedField !== undefined ? { value: pickedField } : {})}
                onValueChange={setPickedField}
                placeholder="Pick a field"
              />
            </Row>
          </Section>

          <Section title="Spinner / Skeleton / Card / Kbd / Code / EmptyState">
            <Row>
              <Spinner size={16} />
              <Skeleton width={120} height={20} radius="pill" />
              <span>
                Press <Kbd>⌘</Kbd> + <Kbd>K</Kbd>
              </span>
              <Code>GET /api/v1/alert</Code>
            </Row>
            <Card padded>
              <EmptyState
                icon="bell-off"
                title="No alerts in this view"
                description="Try widening your time window."
                action={
                  <Button variant="primary" leadingIcon="refresh">
                    Refresh
                  </Button>
                }
              />
            </Card>
          </Section>
        </main>
        <Toaster />
      </ToastProvider>
    </TooltipProvider>
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
