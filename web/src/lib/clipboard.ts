/**
 * Write `text` to the system clipboard. Returns `true` on success, `false`
 * when the Clipboard API is unavailable or rejects (insecure context, denied
 * permission, …). Callers decide how to surface the outcome (toast, etc.).
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through */
  }
  return false;
}
