import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Button } from "@/shared/ui/Button";
import { SILENCING_GUIDE, SILENCING_GUIDE_NOTE } from "./silencingGuide";
import styles from "./SilencingGuideDialog.module.css";

/**
 * "Close, snooze or shelve?" — the long form of the four one-line hints the
 * alert kebab now carries. It exists because the kebab can only afford a
 * sentence per verb, and the real question an operator has ("which of these
 * four do I want, and can I take it back?") needs a comparison, not four
 * separate blurbs.
 *
 * Every claim here is checked against the Go pipeline — see the citation block
 * at the top of silencingGuide.ts, which is the single source for this table,
 * the menu descriptions and the ShelveDialog hint.
 */
export function SilencingGuideDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={styles.content!}>
        <DialogTitle>Close, snooze or shelve?</DialogTitle>
        <DialogBody>
          <DialogDescription>
            Four verbs make an alert stop bothering you, in four different ways.
          </DialogDescription>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Verb</th>
                <th scope="col">What it does</th>
                <th scope="col">Affects</th>
                <th scope="col">How to undo</th>
              </tr>
            </thead>
            <tbody>
              {SILENCING_GUIDE.map((row) => (
                <tr key={row.verb}>
                  <th scope="row" className={styles.verb}>
                    {row.verb}
                  </th>
                  {/* data-label carries the column name into the stacked
                      mobile layout, where the <thead> is hidden. */}
                  <td data-label="What it does">{row.what}</td>
                  <td data-label="Affects">{row.affects}</td>
                  <td data-label="How to undo">{row.undo}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className={styles.note}>{SILENCING_GUIDE_NOTE}</p>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
