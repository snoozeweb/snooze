// Hardcoded widget subtype catalogue.
//
// The Go backend currently has no widget-subtype metadata endpoint and the
// React app does not render widgets in the dashboard yet (this is an admin
// surface only). The old Vue codebase only ever shipped one subtype —
// `patlite` — so a full schema-driven editor is not yet justified. This file
// hardcodes the known subtype(s) so the admin drawer can render a typed form
// while still falling back to a free-form JSON config for any other type.
//
// When more subtypes get defined on the Go side, prefer wiring a real
// metadata endpoint over extending this list indefinitely.

export type WidgetField =
  | {
      name: string;
      label: string;
      description?: string;
      kind: "string";
      required?: boolean;
      default?: string;
    }
  | {
      name: string;
      label: string;
      description?: string;
      kind: "int";
      required?: boolean;
      default?: number;
    };

export type WidgetDef = {
  type: string;
  displayName: string;
  description?: string;
  fields: WidgetField[];
};

// Patlite — mirrors internal/pluginimpl/patlite/metadata.yaml. Only the two
// connection fields (host, port) are exposed here; the more advanced knobs
// (path, timeout, tls_insecure, severity_map) stay JSON-only for now since
// they are rarely changed and would clutter the typed surface.
const PATLITE: WidgetDef = {
  type: "patlite",
  displayName: "Patlite",
  description: "Patlite tower-light device controlled over HTTP",
  fields: [
    {
      name: "host",
      label: "Host",
      description: "Host address of the Patlite device",
      kind: "string",
      required: true,
    },
    {
      name: "port",
      label: "Port",
      description: "HTTP port of the Patlite device",
      kind: "int",
      default: 80,
    },
  ],
};

export const KNOWN_WIDGETS: WidgetDef[] = [PATLITE];

export function findWidgetDef(type: string | undefined | null): WidgetDef | undefined {
  if (!type) return undefined;
  return KNOWN_WIDGETS.find((w) => w.type === type);
}
