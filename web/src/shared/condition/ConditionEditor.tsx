import { useEffect, useRef, useState } from "react";
import * as Tabs from "@radix-ui/react-tabs";
import { Button } from "@/shared/ui/Button";
import type { Condition } from "@/lib/condition/types";
import { encodeText, parseText } from "@/lib/condition/text";
import { ConditionGroup } from "./ConditionGroup";
import { useFieldSuggestions } from "./useFieldSuggestions";
import styles from "./ConditionEditor.module.css";

export type ConditionEditorProps = {
  value: Condition;
  onChange: (next: Condition) => void;
  plugin: string;
};

type Mode = "builder" | "text";

export function ConditionEditor({ value, onChange, plugin }: ConditionEditorProps) {
  const { fields } = useFieldSuggestions(plugin);
  const [mode, setMode] = useState<Mode>("builder");
  const [text, setText] = useState<string>(() => encodeText(value));
  const [parseError, setParseError] = useState<string | null>(null);
  const lastSyncedAst = useRef<Condition>(value);

  // When the parent updates the AST while we're in Builder mode, sync the text state.
  useEffect(() => {
    if (mode === "builder") {
      setText(encodeText(value));
      lastSyncedAst.current = value;
      setParseError(null);
    }
  }, [value, mode]);

  function handleModeChange(next: string) {
    if (next === "text") {
      setText(encodeText(value));
      setMode("text");
      setParseError(null);
      return;
    }
    if (next === "builder") {
      const r = parseText(text);
      if (!r.ok) {
        setParseError(`${r.error.message} (col ${r.error.pos + 1})`);
        return;
      }
      setParseError(null);
      setMode("builder");
      onChange(r.value);
    }
  }

  function handleTextChange(v: string) {
    setText(v);
    const r = parseText(v);
    setParseError(r.ok ? null : `${r.error.message} (col ${r.error.pos + 1})`);
  }

  return (
    <Tabs.Root value={mode} onValueChange={handleModeChange} className={styles.editor}>
      <Tabs.List className={styles.tabs} aria-label="Condition editor mode">
        <Tabs.Trigger value="builder" className={styles.tab}>
          Builder
        </Tabs.Trigger>
        <Tabs.Trigger value="text" className={styles.tab}>
          Text
        </Tabs.Trigger>
      </Tabs.List>
      <Tabs.Content value="builder">
        {value.type === "ALWAYS_TRUE" ? (
          <div className={styles.empty}>
            <span>Always (matches all rows).</span>
            <Button
              size="sm"
              variant="secondary"
              leadingIcon="plus"
              onClick={() =>
                onChange({
                  type: "AND",
                  args: [{ type: "EQUALS", field: "", value: "" }],
                })
              }
            >
              Add filter
            </Button>
          </div>
        ) : (
          <ConditionGroup value={value} fieldOptions={fields} onChange={onChange} isRoot />
        )}
      </Tabs.Content>
      <Tabs.Content value="text">
        <textarea
          aria-label="Condition text"
          className={styles.textarea}
          value={text}
          onChange={(e) => handleTextChange(e.target.value)}
          rows={5}
          spellCheck={false}
        />
        {parseError ? (
          <div className={styles.error} role="alert">
            {parseError}
          </div>
        ) : null}
      </Tabs.Content>
    </Tabs.Root>
  );
}
