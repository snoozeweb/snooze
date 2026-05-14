import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import type { ListResponse } from "@/lib/api/resource";

type Doc = Record<string, unknown>;

function collectFields(doc: Doc, prefix: string, into: Set<string>) {
  for (const [k, v] of Object.entries(doc)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v !== null && typeof v === "object" && !Array.isArray(v) && prefix === "") {
      collectFields(v as Doc, key, into);
    } else {
      into.add(key);
    }
  }
}

export function useFieldSuggestions(plugin: string): {
  fields: string[];
  isPending: boolean;
} {
  const q = useQuery<ListResponse<Doc>>({
    queryKey: ["field-suggestions", plugin],
    queryFn: ({ signal }) =>
      api<ListResponse<Doc>>("GET", `/${plugin}`, {
        query: { limit: 50 },
        signal,
      }),
    staleTime: 5 * 60_000,
  });

  if (!q.data) {
    return { fields: [], isPending: q.isPending };
  }
  const set = new Set<string>();
  for (const doc of q.data.data) {
    collectFields(doc, "", set);
  }
  return { fields: [...set].sort(), isPending: q.isPending };
}
