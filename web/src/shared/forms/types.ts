// Metadata wire shapes returned by /api/v1/metadata.
// Mirrors internal/plugins/metadata.go's Metadata + FormField.

export type FormFieldComponent =
  | "String"
  | "Number"
  | "Text"
  | "Password"
  | "Selector"
  | "Radio"
  | "Switch"
  | "Boolean"
  | "Arguments"
  | "Object";

export type FormFieldOption = {
  text: string;
  value: unknown;
};

export type FormField = {
  display_name: string;
  component: FormFieldComponent;
  description?: string;
  required?: boolean;
  default_value?: unknown;
  options?: FormFieldOption[];
  // For Arguments: a 2-element [keyLabel, valueLabel] hints a key/value map;
  // any other shape hints a flat list of strings.
  placeholder?: unknown;
  // Logical bucket the field belongs to. Used by the Settings page to render
  // grouped pickers (general vs notification); ignored elsewhere.
  group?: string;
};

export type Metadata = {
  // plugin_name is the registry key (Plugin.Name() on the server) — a stable
  // machine-readable identifier injected by the metadata HTTP handler. Use
  // this to match an Action's `action.selected` against the plugin catalogue.
  // The separate `name` field is a human label taken from the plugin's
  // YAML `name:` (e.g. "Send email", "Run a script"), which can't be relied
  // on for matching.
  plugin_name: string;
  name: string;
  display_name?: string;
  icon?: string;
  // Gallery bucket: chat|oncall|ticketing|push|sms|generic (empty → generic).
  category?: string;
  // Canonical setup-docs URL, rendered as a "Docs" link in the editor.
  doc_url?: string;
  // Optional companion daemon that backs this integration with bidirectional /
  // stateful features beyond the in-process notifier. Presence drives the
  // built-in-vs-advanced chooser; it implies no live detection.
  daemon?: { name: string; blurb: string; doc_url: string };
  desc?: string;
  action_form?: Record<string, FormField>;
  setting_form?: Record<string, FormField>;
  widgets?: Record<string, FormField>;
};
