import { useMemo, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { EmptyState } from "@/shared/ui/EmptyState";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { ActionEditor } from "./ActionEditor";
import { Actions, Notifications } from "./api";
import { actionColumns, notificationColumns, notificationRowDisabled } from "./columns";
import { NotificationEditor } from "./NotificationEditor";
import type { Action, Notification } from "./types";
import styles from "./NotificationsPage.module.css";

type PageSearch = BaseListSearch & {
  tab?: "notifications" | "actions";
};

const PAGE_SIZE = 50;

export function NotificationsPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as PageSearch;

  const tab = search.tab ?? "notifications";
  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  // Each tab is its own resource with its own selection + delete state, so we
  // wire the shared scaffolding once per resource. Both share the page's URL,
  // so either `updateSearch` is interchangeable — we use the notification one.
  const removeNotification = Notifications.useRemove();
  const removeAction = Actions.useRemove();
  const notif = useResourceListPage<Notification, PageSearch>({
    to: "/web/notifications",
    remove: removeNotification,
    noun: "notification",
  });
  const action = useResourceListPage<Action, PageSearch>({
    to: "/web/notifications",
    remove: removeAction,
    noun: "action",
  });
  const updateSearch = notif.updateSearch;

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
    placeholder: "action.selected = mail",
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

  // Tabbed header actions — bulk-action bar when rows are selected,
  // otherwise the count + "+ New" affordance. The active tab decides
  // which selection set drives the rendering.
  const activeNotifRows = useMemo(
    () => (notifList.data?.data ?? []).filter((r) => notif.selectedKeys.has(r.uid ?? r.name)),
    [notifList.data, notif.selectedKeys],
  );
  const activeActionRows = useMemo(
    () => (actionList.data?.data ?? []).filter((r) => action.selectedKeys.has(r.uid ?? r.name)),
    [actionList.data, action.selectedKeys],
  );
  const activeSelectedCount =
    tab === "notifications" ? activeNotifRows.length : activeActionRows.length;
  // Toolbar pieces — rendered next to the SearchBar inside DataTable for
  // both tabs so the page chrome matches every other list page.
  const toolbarHeader =
    activeSelectedCount > 0
      ? `${activeSelectedCount} selected`
      : `${list.data?.meta.total ?? 0} ${tab}`;
  const toolbarActions =
    activeSelectedCount > 0 ? (
      tab === "notifications" ? (
        notif.bulkActions(activeNotifRows)
      ) : (
        action.bulkActions(activeActionRows)
      )
    ) : (
      <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
        New
      </Button>
    );

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
          {tab === "notifications" ? (
            <DataTable<Notification>
              data={notifList.data?.data ?? []}
              columns={notificationColumns}
              rowKey={(r) => r.uid ?? r.name}
              rowDisabled={notificationRowDisabled}
              loading={notifList.isPending}
              contextMenuItems={notif.contextMenuItems}
              selectable
              selectedKeys={notif.selectedKeys}
              onSelectionChange={notif.setSelectedKeys}
              search={notifSearch.searchProp}
              toolbarHeader={toolbarHeader}
              toolbar={toolbarActions}
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
              contextMenuItems={action.contextMenuItems}
              selectable
              selectedKeys={action.selectedKeys}
              onSelectionChange={action.setSelectedKeys}
              search={actionSearch.searchProp}
              toolbarHeader={toolbarHeader}
              toolbar={toolbarActions}
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
        state={notif.confirmDelete.state}
        onCancel={notif.confirmDelete.cancel}
        onConfirm={() => void notif.confirmDelete.confirm()}
      />
      <ConfirmDeleteDialog
        state={action.confirmDelete.state}
        onCancel={action.confirmDelete.cancel}
        onConfirm={() => void action.confirmDelete.confirm()}
      />
    </div>
  );
}
