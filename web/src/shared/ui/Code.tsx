import { useCallback, useState } from "react";
import type { ReactNode } from "react";
import styles from "./Code.module.css";

export function Code({ children, className }: { children: ReactNode; className?: string }) {
  const classes = [styles.code, className].filter(Boolean).join(" ");
  return <code className={classes}>{children}</code>;
}

export type CodeBlockProps = {
  children: string;
  copyable?: boolean;
  className?: string;
};

export function CodeBlock({ children, copyable, className }: CodeBlockProps) {
  const [copied, setCopied] = useState(false);
  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(children);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard may be unavailable in tests / iframes; swallow silently.
    }
  }, [children]);
  const classes = [styles.block, className].filter(Boolean).join(" ");
  return (
    <pre className={classes}>
      {copyable ? (
        <button type="button" className={styles.copyBtn} onClick={() => void handleCopy()}>
          {copied ? "Copied" : "Copy"}
        </button>
      ) : null}
      <code>{children}</code>
    </pre>
  );
}
