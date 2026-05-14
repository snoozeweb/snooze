import { forwardRef } from "react";
import type { TextareaHTMLAttributes } from "react";
import styles from "./Textarea.module.css";

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & {
  invalid?: boolean;
};

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { invalid, className, ...rest },
  ref,
) {
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
});
