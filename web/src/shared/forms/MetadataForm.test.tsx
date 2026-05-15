import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { useState } from "react";
import { MetadataField, MetadataForm } from "./MetadataForm";
import type { FormField } from "./types";

function Harness({
  fields,
  initial = {},
  onChange,
}: {
  fields: Record<string, FormField>;
  initial?: Record<string, unknown>;
  onChange?: (next: Record<string, unknown>) => void;
}) {
  const [value, setValue] = useState<Record<string, unknown>>(initial);
  return (
    <MetadataForm
      fields={fields}
      value={value}
      onChange={(next) => {
        setValue(next);
        onChange?.(next);
      }}
    />
  );
}

describe("MetadataForm", () => {
  it("renders String, marks required with asterisk, and shows description", () => {
    const fields: Record<string, FormField> = {
      url: {
        display_name: "URL",
        component: "String",
        required: true,
        description: "Target URL",
      },
    };
    render(<Harness fields={fields} />);
    const input = screen.getByLabelText(/URL/);
    expect(input).toBeInTheDocument();
    expect(input).toHaveAttribute("type", "text");
    expect(screen.getByText(/Target URL/)).toBeInTheDocument();
    // Required asterisk should render inside the label
    expect(screen.getByText("*")).toBeInTheDocument();
  });

  it("fires onChange for String input as plain string", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      url: { display_name: "URL", component: "String" },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} onChange={onChange} />);
    await user.type(screen.getByLabelText(/URL/), "a");
    expect(onChange).toHaveBeenLastCalledWith({ url: "a" });
  });

  it("renders Number and parses to number on change", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      port: { display_name: "Port", component: "Number" },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} onChange={onChange} />);
    const input = screen.getByLabelText(/Port/);
    expect(input).toHaveAttribute("type", "number");
    await user.type(input, "42");
    expect(onChange).toHaveBeenLastCalledWith({ port: 42 });
  });

  it("renders Text as textarea", () => {
    const fields: Record<string, FormField> = {
      body: { display_name: "Body", component: "Text" },
    };
    render(<Harness fields={fields} />);
    const ta = screen.getByLabelText(/Body/);
    expect(ta.tagName.toLowerCase()).toBe("textarea");
  });

  it("renders Password as masked input", () => {
    const fields: Record<string, FormField> = {
      pwd: { display_name: "Password", component: "Password" },
    };
    render(<Harness fields={fields} />);
    expect(screen.getByLabelText(/Password/)).toHaveAttribute("type", "password");
  });

  it("renders Selector with options and fires onChange with the selected value", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      mode: {
        display_name: "Mode",
        component: "Selector",
        options: [
          { text: "One", value: 1 },
          { text: "Two", value: 2 },
        ],
      },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} onChange={onChange} />);
    const select = screen.getByLabelText(/Mode/);
    expect(select.tagName.toLowerCase()).toBe("select");
    await user.selectOptions(select, "2");
    expect(onChange).toHaveBeenLastCalledWith({ mode: 2 });
  });

  it("renders Radio options and fires onChange with the selected value", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      type: {
        display_name: "Type",
        component: "Radio",
        options: [
          { text: "Html", value: "html" },
          { text: "Plain", value: "plain" },
        ],
      },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} onChange={onChange} />);
    await user.click(screen.getByLabelText(/Html/));
    expect(onChange).toHaveBeenLastCalledWith({ type: "html" });
  });

  it("renders Switch and fires onChange with boolean", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      tls: { display_name: "TLS", component: "Switch" },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} onChange={onChange} />);
    await user.click(screen.getByRole("switch", { name: /TLS/ }));
    expect(onChange).toHaveBeenLastCalledWith({ tls: true });
  });

  it("renders Boolean as native checkbox and fires onChange with boolean", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      enabled: { display_name: "Enabled", component: "Boolean" },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} onChange={onChange} />);
    const cb = screen.getByLabelText(/Enabled/);
    expect(cb).toHaveAttribute("type", "checkbox");
    await user.click(cb);
    expect(onChange).toHaveBeenLastCalledWith({ enabled: true });
  });

  it("renders Arguments as key/value rows when placeholder hints a map", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      headers: {
        display_name: "Headers",
        component: "Arguments",
        placeholder: ["header_name", "header_value"],
      },
    };
    const user = userEvent.setup();
    render(
      <Harness
        fields={fields}
        initial={{ headers: { "X-Token": "abc" } }}
        onChange={onChange}
      />,
    );
    // initial row should be visible
    expect(screen.getByDisplayValue("X-Token")).toBeInTheDocument();
    expect(screen.getByDisplayValue("abc")).toBeInTheDocument();
    // Add a row
    await user.click(screen.getByRole("button", { name: /add row/i }));
    // We now have two rows; type into the new key input
    const keyInputs = screen.getAllByPlaceholderText("header_name");
    await user.type(keyInputs[keyInputs.length - 1]!, "Y");
    const last = onChange.mock.calls.at(-1)?.[0] as { headers: Record<string, string> };
    expect(last.headers).toMatchObject({ "X-Token": "abc", Y: "" });
  });

  it("renders Arguments as a string-list when placeholder is a single value", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      command: {
        display_name: "Command",
        component: "Arguments",
      },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} initial={{ command: ["echo"] }} onChange={onChange} />);
    expect(screen.getByDisplayValue("echo")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /add row/i }));
    const inputs = screen.getAllByRole("textbox");
    await user.type(inputs[inputs.length - 1]!, "hi");
    const last = onChange.mock.calls.at(-1)?.[0] as { command: string[] };
    expect(last.command).toEqual(["echo", "hi"]);
  });

  it("renders Object as a JSON textarea and parses JSON on change", async () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      env: { display_name: "Env", component: "Object" },
    };
    const user = userEvent.setup();
    render(<Harness fields={fields} initial={{ env: { A: "1" } }} onChange={onChange} />);
    const ta = screen.getByLabelText<HTMLTextAreaElement>(/Env/);
    expect(ta.value).toMatch(/"A": "1"/);
    await user.clear(ta);
    await user.type(ta, '{{"B":"2"}');
    const last = onChange.mock.calls.at(-1)?.[0] as { env: unknown };
    expect(last.env).toEqual({ B: "2" });
  });

  // MetadataField is the per-field renderer that MetadataForm composes; it
  // is also exposed as a public API so callers that own their own label /
  // grouping (e.g. the Settings editor) can render one field at a time.
  describe("MetadataField (single-field renderer)", () => {
    function FieldHarness({
      field,
      initial,
      onChange,
    }: {
      field: FormField;
      initial?: unknown;
      onChange?: (v: unknown) => void;
    }) {
      const [value, setValue] = useState<unknown>(initial);
      return (
        <MetadataField
          field={field}
          value={value}
          onChange={(v) => {
            setValue(v);
            onChange?.(v);
          }}
          id="mf-test"
        />
      );
    }

    it("renders a String input that fires onChange with strings", async () => {
      const onChange = vi.fn();
      const user = userEvent.setup();
      render(
        <FieldHarness
          field={{ display_name: "URL", component: "String" }}
          onChange={onChange}
        />,
      );
      await user.type(document.getElementById("mf-test") as HTMLInputElement, "ab");
      expect(onChange).toHaveBeenLastCalledWith("ab");
    });

    it("renders a Switch and toggles to boolean true", async () => {
      const onChange = vi.fn();
      const user = userEvent.setup();
      render(
        <FieldHarness
          field={{ display_name: "TLS", component: "Switch" }}
          initial={false}
          onChange={onChange}
        />,
      );
      await user.click(screen.getByRole("switch"));
      expect(onChange).toHaveBeenLastCalledWith(true);
    });

    it("does NOT fall back to default_value on undefined (caller owns state)", () => {
      // Standalone MetadataField shouldn't pretend the value is the default
      // when the caller passes undefined — that would make clear() snap back
      // to the default mid-edit. MetadataForm seeds the default upstream;
      // standalone callers (Settings editor) do the same.
      render(
        <FieldHarness
          field={{
            display_name: "Method",
            component: "String",
            default_value: "POST",
          }}
        />,
      );
      const input = document.getElementById("mf-test") as HTMLInputElement;
      expect(input.value).toBe("");
    });
  });

  it("applies default_value only when the current value is undefined", () => {
    const onChange = vi.fn();
    const fields: Record<string, FormField> = {
      method: {
        display_name: "Method",
        component: "String",
        default_value: "POST",
      },
    };
    function Wrapper() {
      const [value, setValue] = useState<Record<string, unknown>>({});
      return (
        <MetadataForm
          fields={fields}
          value={value}
          onChange={(next) => {
            setValue(next);
            onChange(next);
          }}
        />
      );
    }
    render(<Wrapper />);
    // The default should be visible
    const input = screen.getByLabelText<HTMLInputElement>(/Method/);
    expect(input.value).toBe("POST");
  });
});
