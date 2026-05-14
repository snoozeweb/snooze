import { useCallback, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { ActionEditor } from "./ActionEditor";
import { Actions, Notifications } from "./api";
import { actionColumns, notificationColumns } from "./columns";
import { NotificationEditor } from "./NotificationEditor";
import styles from "./NotificationsPage.module.css";

type PageSearch = {
  tab?: "notifications" | "actions";
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
  search: (prev: PageSearch | undefined) => PageSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

export function NotificationsPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as PageSearch;
  const navigate = useNavigate();

  const tab = search.tab ?? "notifications";
  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: PageSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/notifications",
        search: (prev: PageSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged; // eslint-disable-line @typescript-eslint/no-unused-vars
            return rest as PageSearch;
          }
          return merged as PageSearch;
        },
      });
    },
    [navigate],
  );

  const notifList = Notifications.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
  });
  const actionList = Actions.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
  });

  const list = tab === "notifications" ? notifList : actionList;

  return (
    <div className={styles.page}>
      <Tabs
        value={tab}
        onValueChange={(v) => updateSearch({ tab: v as "notifications" | "actions", page: 1 })}
      >
        <TabList>
          <TabTrigger value="notifications">Notifications</TabTrigger>
          <TabTrigger value="actions">Actions</TabTrigger>
        </TabList>
        <TabPanel value={tab}>
          <div className={styles.topbar}>
            <span style={{ color: "var(--text-muted)", fontSize: "var(--text-sm)" }}>
              {list.data?.meta.total ?? 0} {tab}
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
          {tab === "notifications" ? (
            <DataTable
              data={notifList.data?.data ?? []}
              columns={notificationColumns}
              rowKey={(r) => r.uid ?? r.name}
              loading={notifList.isPending}
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
                total: notifList.data?.meta.total ?? 0,
                onChange: (next) => updateSearch({ page: next.page }),
              }}
              onRowOpen={(row) => {
                if (row.uid) updateSearch({ uid: row.uid });
              }}
            />
          ) : (
            <DataTable
              data={actionList.data?.data ?? []}
              columns={actionColumns}
              rowKey={(r) => r.uid ?? r.name}
              loading={actionList.isPending}
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
                total: actionList.data?.meta.total ?? 0,
                onChange: (next) => updateSearch({ page: next.page }),
              }}
              onRowOpen={(row) => {
                if (row.uid) updateSearch({ uid: row.uid });
              }}
            />
          )}
        </TabPanel>
      </Tabs>

      {tab === "notifications" && detailUid !== undefined ? (
        <NotificationEditor uid={detailUid} onClose={() => updateSearch({})} />
      ) : null}
      {tab === "actions" && detailUid !== undefined ? (
        <ActionEditor uid={detailUid} onClose={() => updateSearch({})} />
      ) : null}
      {creating && tab === "notifications" ? (
        <NotificationEditor uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
      {creating && tab === "actions" ? (
        <ActionEditor uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
    </div>
  );
}
