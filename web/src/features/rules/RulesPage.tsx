import { useCallback, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { AggregateRules, Rules } from "./api";
import { RuleEditor } from "./RuleEditor";
import { ruleColumns } from "./columns";
import type { Rule } from "./types";
import styles from "./RulesPage.module.css";

type RulesSearch = {
  tab?: "rules" | "aggregates";
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn = (opts: {
  to: string;
  search: (prev: RulesSearch | undefined) => RulesSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

export function RulesPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as RulesSearch;
  const navigate = useNavigate();

  const tab = search.tab ?? "rules";
  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: RulesSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/rules",
        search: (prev: RulesSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged; // eslint-disable-line @typescript-eslint/no-unused-vars
            return rest as RulesSearch;
          }
          return merged as RulesSearch;
        },
      });
    },
    [navigate],
  );

  const resource = tab === "rules" ? Rules : AggregateRules;
  const list = resource.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
  });

  const editorPlugin: "rule" | "aggregaterule" = tab === "rules" ? "rule" : "aggregaterule";

  return (
    <div className={styles.page}>
      <Tabs
        value={tab}
        onValueChange={(v) => updateSearch({ tab: v as "rules" | "aggregates", page: 1 })}
      >
        <TabList>
          <TabTrigger value="rules">Rules</TabTrigger>
          <TabTrigger value="aggregates">Aggregates</TabTrigger>
        </TabList>
        <TabPanel value={tab}>
          <div className={styles.topbar}>
            <span style={{ color: "var(--text-muted)", fontSize: "var(--text-sm)" }}>
              {list.data?.meta.total ?? 0} {tab === "rules" ? "rules" : "aggregate rules"}
            </span>
            <Button
              size="sm"
              variant="primary"
              leadingIcon="plus"
              onClick={() => setCreating(true)}
            >
              New
            </Button>
          </div>
          <DataTable<Rule>
            data={list.data?.data ?? []}
            columns={ruleColumns}
            rowKey={(r) => r.uid ?? r.name}
            loading={list.isPending}
            serverSort={{
              sortBy: orderby,
              order: asc ? "asc" : "desc",
              onChange: (next) =>
                updateSearch({
                  orderby: next.sortBy,
                  asc: next.order === "asc",
                  page: 1,
                }),
            }}
            serverPagination={{
              page,
              pageSize: PAGE_SIZE,
              total: list.data?.meta.total ?? 0,
              onChange: (next) => updateSearch({ page: next.page }),
            }}
            onRowOpen={(row) => {
              if (row.uid) updateSearch({ uid: row.uid });
            }}
          />
        </TabPanel>
      </Tabs>

      {detailUid !== undefined ? (
        <RuleEditor plugin={editorPlugin} uid={detailUid} onClose={() => updateSearch({})} />
      ) : null}

      {creating ? (
        <RuleEditor plugin={editorPlugin} uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
    </div>
  );
}
