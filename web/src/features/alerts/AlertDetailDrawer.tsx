import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { Drawer, DrawerBody, DrawerContent, DrawerTitle } from "@/shared/ui/Drawer";
import { Spinner } from "@/shared/ui/Spinner";
import { Records } from "./api";
import { CommentTimeline } from "./CommentTimeline";
import { formatRelativeTime, severityBadgeVariant, stateBadgeVariant, stateLabel } from "./format";
import { GrafanaPane } from "./info/GrafanaPane";
import { MailPane } from "./info/MailPane";
import { PrometheusPane } from "./info/PrometheusPane";
import type { AlertState } from "./types";
import styles from "./AlertDetailDrawer.module.css";

export type AlertDetailDrawerProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function AlertDetailDrawer({ uid, onClose }: AlertDetailDrawerProps) {
  const open = uid !== undefined;
  const q = Records.useGet(uid);
  const record = q.data;

  return (
    <Drawer
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{record?.host ?? uid ?? "Alert"}</DrawerTitle>
        <DrawerBody>
          {q.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : record ? (
            <div className={styles.stack}>
              <section className={styles.section}>
                <span className={styles.uidRow}>
                  <Code>{record.uid ?? ""}</Code>
                </span>
                <div className={styles.props}>
                  {record.host !== undefined ? (
                    <>
                      <span className={styles.propLabel}>Host</span>
                      <span className={styles.propValue}>
                        <Code>{record.host}</Code>
                      </span>
                    </>
                  ) : null}
                  {record.source !== undefined ? (
                    <>
                      <span className={styles.propLabel}>Source</span>
                      <span className={styles.propValue}>{record.source}</span>
                    </>
                  ) : null}
                  {record.severity !== undefined ? (
                    <>
                      <span className={styles.propLabel}>Severity</span>
                      <span className={styles.propValue}>
                        <Badge variant={severityBadgeVariant(record.severity)}>
                          {record.severity}
                        </Badge>
                      </span>
                    </>
                  ) : null}
                  {record.state !== undefined ? (
                    <>
                      <span className={styles.propLabel}>State</span>
                      <span className={styles.propValue}>
                        <Badge variant={stateBadgeVariant(record.state as AlertState)}>
                          {stateLabel(record.state as AlertState)}
                        </Badge>
                      </span>
                    </>
                  ) : null}
                  {record.environment !== undefined ? (
                    <>
                      <span className={styles.propLabel}>Environment</span>
                      <span className={styles.propValue}>{record.environment}</span>
                    </>
                  ) : null}
                  <span className={styles.propLabel}>When</span>
                  <span className={styles.propValue}>{formatRelativeTime(record.date_epoch)}</span>
                  {record.message ? (
                    <>
                      <span className={styles.propLabel}>Message</span>
                      <span className={styles.propValue}>{record.message}</span>
                    </>
                  ) : null}
                  {record.tags && record.tags.length > 0 ? (
                    <>
                      <span className={styles.propLabel}>Tags</span>
                      <span className={styles.tags}>
                        {record.tags.map((t) => (
                          <Badge key={t} variant="muted">
                            {t}
                          </Badge>
                        ))}
                      </span>
                    </>
                  ) : null}
                </div>
              </section>

              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Timeline</h3>
                <CommentTimeline recordUid={record.uid} />
              </section>

              <MailPane record={record} />
              <GrafanaPane record={record} />
              <PrometheusPane record={record} />
            </div>
          ) : (
            <p>Not found.</p>
          )}
        </DrawerBody>
      </DrawerContent>
    </Drawer>
  );
}
