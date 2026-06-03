import { useMemo, useState } from "react";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Tabs, TabList, TabPanel, TabTrigger } from "@/shared/ui/Tabs";
import { Code, CodeBlock } from "@/shared/ui/Code";
import { Button } from "@/shared/ui/Button";
import { Icon } from "@/shared/icons/Icon";
import { docsUrl } from "@/lib/docs";
import {
  type InjectionFamily,
  type InjectionSource,
  REST_SOURCE,
  sourcesForFamily,
} from "./injectionGuide";
import styles from "./InjectAlertsDialog.module.css";

export type InjectAlertsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

// The base URL used in copy-pasteable snippets. Prefer the live origin so the
// curl/URLs run against this very server; fall back to a placeholder in
// non-browser contexts (tests, SSR).
function resolveBaseUrl(): string {
  if (typeof window !== "undefined" && window.location?.origin) {
    return window.location.origin;
  }
  return "https://snooze.example.com";
}

function SourcePanel({ source, baseUrl }: { source: InjectionSource; baseUrl: string }) {
  return (
    <div className={styles.panel}>
      {source.endpoint ? (
        <p className={styles.endpoint}>
          <Code>{source.endpoint}</Code>
        </p>
      ) : null}
      <p className={styles.summary}>{source.summary}</p>
      <CodeBlock copyable>{source.snippet(baseUrl)}</CodeBlock>
      <a className={styles.docLink} href={docsUrl(source.docSlug)} target="_blank" rel="noreferrer">
        <Icon name="book" size={14} /> Full {source.name} docs <span aria-hidden="true">↗</span>
      </a>
    </div>
  );
}

function FamilyBrowser({ family, baseUrl }: { family: InjectionFamily; baseUrl: string }) {
  const sources = sourcesForFamily(family);
  const [selectedId, setSelectedId] = useState(sources[0]?.id ?? "");
  const selected = sources.find((s) => s.id === selectedId) ?? sources[0];
  return (
    <div className={styles.browser}>
      <div className={styles.picker} role="group" aria-label={`${family} sources`}>
        {sources.map((s) => (
          <button
            key={s.id}
            type="button"
            aria-pressed={s.id === selected?.id}
            className={s.id === selected?.id ? styles.pickerItemActive : styles.pickerItem}
            onClick={() => setSelectedId(s.id)}
          >
            {s.name}
          </button>
        ))}
      </div>
      {selected ? <SourcePanel source={selected} baseUrl={baseUrl} /> : null}
    </div>
  );
}

export function InjectAlertsDialog({ open, onOpenChange }: InjectAlertsDialogProps) {
  const baseUrl = useMemo(() => resolveBaseUrl(), []);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* styles.content is `string | undefined` under noUncheckedIndexedAccess;
          the class is always present, and DialogContent's className is a plain
          `string?` (exactOptionalPropertyTypes rejects an explicit undefined). */}
      <DialogContent className={styles.content!}>
        <DialogTitle>How to inject alerts</DialogTitle>
        <DialogBody>
          <DialogDescription>
            Connect a monitoring source to start ingesting alerts. Pick how your source talks to
            Snooze.
          </DialogDescription>
          <Tabs defaultValue="rest">
            <TabList>
              <TabTrigger value="rest">REST API</TabTrigger>
              <TabTrigger value="webhook">Webhooks</TabTrigger>
              <TabTrigger value="daemon">Daemon inputs</TabTrigger>
            </TabList>
            <TabPanel value="rest">
              <SourcePanel source={REST_SOURCE} baseUrl={baseUrl} />
            </TabPanel>
            <TabPanel value="webhook">
              <FamilyBrowser family="webhook" baseUrl={baseUrl} />
            </TabPanel>
            <TabPanel value="daemon">
              <FamilyBrowser family="daemon" baseUrl={baseUrl} />
            </TabPanel>
          </Tabs>
        </DialogBody>
        <DialogFooter>
          <a
            className={styles.guideLink}
            href={docsUrl("general/integrations/sending-alerts")}
            target="_blank"
            rel="noreferrer"
          >
            <Icon name="book" size={14} /> Send your first alert — full guide{" "}
            <span aria-hidden="true">↗</span>
          </a>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
