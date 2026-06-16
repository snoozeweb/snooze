// CopyField — read-only value + a one-click copy button. Used to show a
// secret (e.g. a freshly minted API key) exactly once: the value is
// selectable on focus and copied to the clipboard via the button, with a
// toast confirming the copy.
import { Button } from "./Button";
import { Input } from "./Input";
import { toast } from "./toast/useToast";

export type CopyFieldProps = {
  value: string;
  label?: string;
  "aria-label"?: string;
};

export function CopyField({ value, label, ...rest }: CopyFieldProps) {
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      toast.success("Copied to clipboard");
    } catch {
      toast.error("Copy failed — select and copy manually");
    }
  }
  return (
    <div style={{ display: "flex", gap: "var(--space-2)", alignItems: "center" }}>
      <Input
        value={value}
        readOnly
        aria-label={rest["aria-label"] ?? label ?? "value"}
        onFocus={(e) => e.currentTarget.select()}
      />
      <Button
        type="button"
        size="md"
        variant="secondary"
        leadingIcon="copy"
        onClick={() => void copy()}
      >
        Copy
      </Button>
    </div>
  );
}
