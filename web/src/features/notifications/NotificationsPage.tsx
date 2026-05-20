import { useCallback, useMemo, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { EmptyState } from "@/shared/ui/EmptyState";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import {
  buildResourceContextMenu,
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { ActionEditor } from "./ActionEditor";
import { Actions, Notifications } from "./api";
import { actionColumns, notificationColumns, notificationRowDisabled } from "./columns";
import { NotificationEditor } from "./NotificationEditor";
import type { Action, Notification } from "./types";
import styles from "./NotificationsPage.module.css";

type PageSearch = {
  tab?: "notifications" | "actions";
  uid?: string | undefined;
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
            const { uid: _uid, ...rest } = merged;
            return rest as PageSearch;
          }
          return merged as PageSearch;
        },
      });
    },
    [navigate],
  );

  // Each tab carries its own search state. Switching tabs preserves the
  // text in whichever tab the user typed in, but the active list endpoint
  // only sees the filter for the currently-shown tab.
  const notifSearch = useTableSearch({
    collection: "notification",
    placeholder: "name = … AND enabled = true",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });
  const actionSearch = useTableSearch({
    collection: "action",
    placeholder: "action_type = mail",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const notifList = Notifications.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(notifSearch.q ? { q: notifSearch.q } : {}),
  });
  const actionList = Actions.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(actionSearch.q ? { q: actionSearch.q } : {}),
  });

  const list = tab === "notifications" ? notifList : actionList;

  const removeNotification = Notifications.useRemove();
  const removeAction = Actions.useRemove();
  const [notifSelected, setNotifSelected] = useState<Set<string>>(new Set());
  const [actionSelected, setActionSelected] = useState<Set<string>>(new Set());

  const confirmDeleteNotif = useConfirmDelete<Notification>({
    onDelete: (uid) => removeNotification.mutateAsync(uid),
    noun: "notification",
    onAfter: () => setNotifSelected(new Set()),
  });
  const confirmDeleteAction = useConfirmDelete<Action>({
    onDelete: (uid) => removeAction.mutateAsync(uid),
    noun: "action",
    onAfter: () => setActionSelected(new Set()),
  });

  const notifMenu = useCallback(
    (row: Notification): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onOpen: (r) => {
          if (r.uid) updateSearch({ uid: r.uid });
        },
        onDelete: (uid) => removeNotification.mutateAsync(uid),
        requestDelete: (r) => confirmDeleteNotif.request([r]),
      }),
    [updateSearch, removeNotification, confirmDeleteNotif],
  );
  const actionMenu = useCallback(
    (row: Action): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onOpen: (r) => {
          if (r.uid) updateSearch({ uid: r.uid });
        },
        onDelete: (uid) => removeAction.mutateAsync(uid),
        requestDelete: (r) => confirmDeleteAction.request([r]),
      }),
    [updateSearch, removeAction, confirmDeleteAction],
  );
  const notifBulk = useCallback(
    (rows: Notification[]) => (
      <Button
        size="sm"
        variant="danger"
        leadingIcon="trash"
        onClick={() => confirmDeleteNotif.request(rows)}
      >
        Delete ({rows.length})
      </Button>
    ),
    [confirmDeleteNotif],
  );
  const actionBulk = useCallback(
    (rows: Action[]) => (
      <Button
        size="sm"
        variant="danger"
        leadingIcon="trash"
        onClick={() => confirmDeleteAction.request(rows)}
      >
        Delete ({rows.length})
      </Button>
    ),
    [confirmDeleteAction],
  );

  // Tabbed header actions — bulk-action bar when rows are selected,
  // otherwise the count + "+ New" affordance. The active tab decides
  // which selection set drives the rendering.
  const activeNotifRows = useMemo(
    () =>
      (notifList.data?.data ?? []).filter((r) => notifSelected.has(r.uid ?? r.name)),
    [notifList.data, notifSelected],
  );
  const activeActionRows = useMemo(
    () =>
      (actionList.data?.data ?? []).filter((r) => actionSelected.has(r.uid ?? r.name)),
    [actionList.data, actionSelected],
  );
  const activeSelectedCount =
    tab === "notifications" ? activeNotifRows.length : activeActionRows.length;
  const headerActions =
    activeSelectedCount > 0 ? (
      <>
        <span className={styles.selectionCount}>{activeSelectedCount} selected</span>
        {tab === "notifications" ? notifBulk(activeNotifRows) : actionBulk(activeActionRows)}
      </>
    ) : (
      <>
        <span className={styles.headerCount}>{list.data?.meta.total ?? 0} {tab}</span>
        <Button
          size="sm"
          variant="primary"
          leadingIcon="plus"
          onClick={() => setCreating(true)}
        >
          New
        </Button>
      </>
    );

  return (
    <div className={styles.page}>
      <Tabs
        value={tab}
        onValueChange={(v) => updateSearch({ tab: v as "notifications" | "actions", page: 1 })}
      >
        <TabList rightSlot={headerActions}>
          <TabTrigger value="notifications">Notifications</TabTrigger>
          <TabTrigger value="actions">Actions</TabTrigger>
        </TabList>
        <TabPanel value={tab}>
          {tab === "notifications" ? (
            <DataTable<Notification>
              data={notifList.data?.data ?? []}
              columns={notificationColumns}
              rowKey={(r) => r.uid ?? r.name}
              rowDisabled={notificationRowDisabled}
              loading={notifList.isPending}
              contextMenuItems={notifMenu}
              selectable
              selectedKeys={notifSelected}
              onSelectionChange={setNotifSelected}
              search={notifSearch.searchProp}
              emptyState={
                <EmptyState
                  icon="file-text"
                  title="No notifications yet"
                  description="Notifications route matching alerts to one or more actions."
                  action={
                    <Button
                      size="md"
                      variant="primary"
                      leadingIcon="plus"
                      onClick={() => setCreating(true)}
                    >
                      New notification
                    </Button>
                  }
                />
              }
              renderExpanded={(row) => (
                <RowDetailPanel
                  row={row as unknown as Record<string, unknown>}
                  objectType="notification"
                  objectId={row.uid}
                />
              )}
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
            <DataTable<Action>
              data={actionList.data?.data ?? []}
              columns={actionColumns}
              rowKey={(r) => r.uid ?? r.name}
              loading={actionList.isPending}
              contextMenuItems={actionMenu}
              selectable
              selectedKeys={actionSelected}
              onSelectionChange={setActionSelected}
              search={actionSearch.searchProp}
              emptyState={
                <EmptyState
                  icon="file-text"
                  title="No actions yet"
                  description="Actions describe how to deliver a notification (mail, webhook, …)."
                  action={
                    <Button
                      size="md"
                      variant="primary"
                      leadingIcon="plus"
                      onClick={() => setCreating(true)}
                    >
                      New action
                    </Button>
                  }
                />
              }
              renderExpanded={(row) => (
                <RowDetailPanel
                  row={row as unknown as Record<string, unknown>}
                  objectType="action"
                  objectId={row.uid}
                />
              )}
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
        <NotificationEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {tab === "actions" && detailUid !== undefined ? (
        <ActionEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating && tab === "notifications" ? (
        <NotificationEditor uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
      {creating && tab === "actions" ? (
        <ActionEditor uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
      <ConfirmDeleteDialog
        state={confirmDeleteNotif.state}
        onCancel={confirmDeleteNotif.cancel}
        onConfirm={() => void confirmDeleteNotif.confirm()}
      />
      <ConfirmDeleteDialog
        state={confirmDeleteAction.state}
        onCancel={confirmDeleteAction.cancel}
        onConfirm={() => void confirmDeleteAction.confirm()}
      />
    </div>
  );
}
