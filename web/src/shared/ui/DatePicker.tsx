// DatePicker — single-date picker that wraps react-day-picker (mode="single")
// in the in-house Radix Popover. The codebase has a range picker
// (DateTimeRangePicker) but no single-date primitive; the API-key expiry
// field needs one.
//
// Wire shape: value/onChange speak the "YYYY-MM-DD" string the backend
// expects (or undefined for "no date"). The Date <-> string conversions stay
// in local time on purpose — a bare date has no timezone, so the calendar
// must display the same day the user picked without UTC drift.
//
// react-day-picker ships its own structural CSS (imported below); the
// .module.css alongside this file only themes the trigger chip.
import { useState } from "react";
import { DayPicker } from "react-day-picker";
import "react-day-picker/style.css";
import { Popover, PopoverContent, PopoverTrigger } from "./Popover";
import { Button } from "./Button";
import styles from "./DatePicker.module.css";

export type DatePickerProps = {
  /** "YYYY-MM-DD" or undefined for no date. */
  value: string | undefined;
  onChange: (next: string | undefined) => void;
  placeholder?: string;
  "aria-label"?: string;
  disabled?: boolean;
};

function toDate(v: string | undefined): Date | undefined {
  if (!v) return undefined;
  const [y, m, d] = v.split("-").map(Number);
  if (!y || !m || !d) return undefined;
  return new Date(y, m - 1, d);
}

function toWire(d: Date | undefined): string | undefined {
  if (!d) return undefined;
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${d.getFullYear()}-${m}-${day}`;
}

export function DatePicker({
  value,
  onChange,
  placeholder = "No expiry (max)",
  disabled,
  ...rest
}: DatePickerProps) {
  const [open, setOpen] = useState(false);
  const selected = toDate(value);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={styles.trigger}
          aria-label={rest["aria-label"]}
          aria-haspopup="dialog"
          disabled={disabled}
        >
          {value ?? placeholder}
        </button>
      </PopoverTrigger>
      <PopoverContent>
        <DayPicker
          mode="single"
          selected={selected}
          onSelect={(d) => {
            onChange(toWire(d));
            setOpen(false);
          }}
          numberOfMonths={1}
          showOutsideDays
        />
        {value ? (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              onChange(undefined);
              setOpen(false);
            }}
          >
            Clear
          </Button>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}
