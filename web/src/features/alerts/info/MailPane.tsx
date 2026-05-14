import { Card } from "@/shared/ui/Card";
import { Icon } from "@/shared/icons/Icon";
import type { Record_ } from "../types";
import styles from "./info.module.css";

type WithSmtp = Record_ & {
  smtp_from?: string;
  smtp_to?: string;
  smtp_subject?: string;
  smtp_body?: string;
};

export function MailPane({ record }: { record: Record_ }) {
  const r = record as WithSmtp;
  if (!r.smtp_from && !r.smtp_subject && !r.smtp_body) return null;

  return (
    <Card padded>
      <div className={styles.pane}>
        <h3 className={styles.title}>
          <Icon name="message-square" size={14} />
          Email
        </h3>
        <div className={styles.grid}>
          {r.smtp_from ? (
            <>
              <span className={styles.label}>From</span>
              <span className={styles.value}>{r.smtp_from}</span>
            </>
          ) : null}
          {r.smtp_to ? (
            <>
              <span className={styles.label}>To</span>
              <span className={styles.value}>{r.smtp_to}</span>
            </>
          ) : null}
          {r.smtp_subject ? (
            <>
              <span className={styles.label}>Subject</span>
              <span className={styles.value}>{r.smtp_subject}</span>
            </>
          ) : null}
        </div>
        {r.smtp_body ? <pre className={styles.preformatted}>{r.smtp_body}</pre> : null}
      </div>
    </Card>
  );
}
