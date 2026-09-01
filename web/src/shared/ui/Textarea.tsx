import { useId, type TextareaHTMLAttributes } from "react";
import type React from "react";
import styles from "./Textarea.module.css";

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & {
  invalid?: boolean;
  /** Visible validation message shown under the field when `invalid` is
   *  true, wired via `aria-describedby` — see Input's `errorMessage` for the
   *  rationale. */
  errorMessage?: string | undefined;
  ref?: React.Ref<HTMLTextAreaElement>;
};

export function Textarea({
  invalid,
  errorMessage,
  className,
  ref,
  id,
  "aria-describedby": describedBy,
  ...rest
}: TextareaProps) {
  const generatedId = useId();
  const errorId = `${id ?? generatedId}-error`;
  const showError = invalid && !!errorMessage;
  const classes = [styles.textarea, invalid ? styles.invalid : null, className]
    .filter(Boolean)
    .join(" ");
  return (
    <>
      <textarea
        ref={ref}
        id={id}
        className={classes}
        {...(invalid ? { "aria-invalid": true } : {})}
        aria-describedby={
          [describedBy, showError ? errorId : undefined].filter(Boolean).join(" ") || undefined
        }
        {...rest}
      />
      {showError ? (
        <p id={errorId} role="alert" className={styles.errorMessage}>
          {errorMessage}
        </p>
      ) : null}
    </>
  );
}
