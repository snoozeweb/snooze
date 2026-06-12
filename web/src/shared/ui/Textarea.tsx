import type { TextareaHTMLAttributes } from "react";
import type React from "react";
import styles from "./Textarea.module.css";

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & {
  invalid?: boolean;
  ref?: React.Ref<HTMLTextAreaElement>;
};

export function Textarea({ invalid, className, ref, ...rest }: TextareaProps) {
  const classes = [styles.textarea, invalid ? styles.invalid : null, className]
    .filter(Boolean)
    .join(" ");
  return (
    <textarea
      ref={ref}
      className={classes}
      {...(invalid ? { "aria-invalid": true } : {})}
      {...rest}
    />
  );
}
