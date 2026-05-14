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
import { toast } from "@/shared/ui/toast/useToast";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { Textarea } from "@/shared/ui/Textarea";
import { Tooltip } from "@/shared/ui/Tooltip";

const fields = [
  { value: "host", label: "host" },
  { value: "message", label: "message" },
  { value: "severity", label: "severity" },
];

export function PrimitivesPage() {
  const [switchOn, setSwitchOn] = useState(false);
  const [pickedField, setPickedField] = useState<string | undefined>(undefined);

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
      <h1>Primitives</h1>

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

      <Section title="Badges">
        <Row>
          <Badge>neutral</Badge>
          <Badge variant="info">info</Badge>
          <Badge variant="ok">ok</Badge>
          <Badge variant="warning">warning</Badge>
          <Badge variant="error">error</Badge>
          <Badge variant="critical">critical</Badge>
        </Row>
      </Section>

      <Section title="Overlays">
        <Row>
          <Tooltip content="Refresh the list">
            <IconButton icon="refresh" label="Refresh" />
          </Tooltip>
          <Popover>
            <PopoverTrigger>
              <Button trailingIcon="chevron-down">Popover</Button>
            </PopoverTrigger>
            <PopoverContent>panel</PopoverContent>
          </Popover>
          <Menu>
            <MenuTrigger>
              <IconButton icon="more-horizontal" label="More" />
            </MenuTrigger>
            <MenuContent>
              <MenuItem leadingIcon="edit">Edit</MenuItem>
              <MenuItem leadingIcon="copy" shortcut="⌘D">
                Duplicate
              </MenuItem>
              <MenuSeparator />
              <MenuItem leadingIcon="trash" danger>
                Delete
              </MenuItem>
            </MenuContent>
          </Menu>
          <Dialog>
            <DialogTrigger>
              <Button variant="danger">Delete</Button>
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
          <Drawer>
            <DrawerTrigger>
              <Button>Drawer</Button>
            </DrawerTrigger>
            <DrawerContent>
              <DrawerTitle>Edit rule</DrawerTitle>
              <DrawerBody>Body</DrawerBody>
              <DrawerFooter>
                <Button>Cancel</Button>
                <Button variant="primary">Save</Button>
              </DrawerFooter>
            </DrawerContent>
          </Drawer>
        </Row>
      </Section>

      <Section title="Form">
        <Row>
          <Input placeholder="Search…" leadingIcon="search" />
          <Select>
            <SelectTrigger placeholder="Pick severity…" />
            <SelectContent>
              <SelectItem value="info">info</SelectItem>
              <SelectItem value="error">error</SelectItem>
              <SelectItem value="critical">critical</SelectItem>
            </SelectContent>
          </Select>
          <Combobox
            options={fields}
            {...(pickedField !== undefined ? { value: pickedField } : {})}
            onValueChange={setPickedField}
            placeholder="Pick field"
          />
        </Row>
        <Row>
          <Switch checked={switchOn} onCheckedChange={setSwitchOn} aria-label="x" />
          <Checkbox aria-label="c" defaultChecked />
          <RadioGroup defaultValue="a">
            <RadioOption value="a" aria-label="A" />
            <RadioOption value="b" aria-label="B" />
          </RadioGroup>
        </Row>
        <Textarea placeholder="Comment" rows={3} />
      </Section>

      <Section title="Tabs">
        <Tabs defaultValue="a">
          <TabList>
            <TabTrigger value="a">A</TabTrigger>
            <TabTrigger value="b">B</TabTrigger>
          </TabList>
          <TabPanel value="a">Panel A</TabPanel>
          <TabPanel value="b">Panel B</TabPanel>
        </Tabs>
      </Section>

      <Section title="Toast">
        <Row>
          <Button onClick={() => toast.success("Saved")}>Toast success</Button>
          <Button variant="danger" onClick={() => toast.error("Boom", { traceId: "abc123" })}>
            Toast error
          </Button>
        </Row>
      </Section>

      <Section title="Misc">
        <Row>
          <Spinner size={16} />
          <Skeleton width={120} height={20} radius="pill" />
          <Code>GET /api/v1/alert</Code>
          <span>
            Press <Kbd>⌘</Kbd> + <Kbd>K</Kbd>
          </span>
        </Row>
        <Card padded>
          <EmptyState
            icon="bell-off"
            title="No alerts in this view"
            description="Widen your time window."
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
